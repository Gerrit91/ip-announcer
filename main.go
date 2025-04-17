package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/metal-stack/metal-go/api/models"
	hammerapi "github.com/metal-stack/metal-hammer/pkg/api"
	"github.com/metal-stack/metal-lib/pkg/net"
	"github.com/metal-stack/metal-lib/pkg/pointer"
	api "github.com/osrg/gobgp/v3/api"
	server "github.com/osrg/gobgp/v3/pkg/server"
	"gopkg.in/yaml.v3"

	"google.golang.org/protobuf/types/known/anypb"
)

const (
	installYAML = "/etc/metal/install.yaml"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, log); err != nil {
		log.Error("error running application", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger) error {
	ip := os.Getenv("CONTROL_PLANE_IP")
	if ip == "" {
		return fmt.Errorf("no ip given to announce")
	}

	data, err := os.ReadFile(installYAML)
	if err != nil {
		return fmt.Errorf("unable to open install.yaml: %w", err)
	}

	var install hammerapi.InstallerConfig
	err = yaml.Unmarshal(data, &install)
	if err != nil {
		return fmt.Errorf("unable to read install.yaml: %w", err)
	}

	idx := slices.IndexFunc(install.Networks, func(n *models.V1MachineNetwork) bool {
		return *n.Networktype == net.PrivatePrimaryUnshared
	})

	if idx < 0 {
		return fmt.Errorf("no private primary unshared network found in install.yaml")
	}

	asn := uint32(pointer.SafeDeref(install.Networks[idx].Asn))

	log.Info("figured out peer asn", "asn", asn)

	s := server.NewBgpServer()
	go s.Serve()

	err = s.StartBgp(ctx, &api.StartBgpRequest{
		Global: &api.Global{
			Asn:        asn,
			ListenPort: -1,
			RouterId:   "127.0.0.1",
		},
	})
	if err != nil {
		return fmt.Errorf("unable to start up bgp server: %w", err)
	}

	log.Info("started bgp server", "port", 5000)

	err = s.AddPeer(ctx, &api.AddPeerRequest{Peer: &api.Peer{
		Conf: &api.PeerConf{
			NeighborAddress: "127.0.0.1",
			PeerAsn:         asn,
		},
		Timers: &api.Timers{
			Config: &api.TimersConfig{
				ConnectRetry:      10,
				HoldTime:          3,
				KeepaliveInterval: 1,
			},
		},
		Transport: &api.Transport{
			MtuDiscovery:  true,
			RemoteAddress: "127.0.0.1",
			RemotePort:    uint32(179),
		},
	}})
	if err != nil {
		return fmt.Errorf("failed to peer with localhost: %v", err)
	}

	log.Info("successfully peered with localhost")

	nlri, _ := anypb.New(&api.IPAddressPrefix{
		Prefix:    ip,
		PrefixLen: 32,
	})
	nhAttr, _ := anypb.New(&api.NextHopAttribute{
		NextHop: "0.0.0.0",
	})
	originAttr, _ := anypb.New(&api.OriginAttribute{
		Origin: 0,
	})

	_, err = s.AddPath(ctx, &api.AddPathRequest{
		Path: &api.Path{
			Nlri: nlri,
			Family: &api.Family{
				Afi:  api.Family_AFI_IP,
				Safi: api.Family_SAFI_UNICAST,
			},
			Pattrs: []*anypb.Any{originAttr, nhAttr},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to announce route: %w", err)
	}

	log.Info("announcing ip address", "ip", ip)

	<-ctx.Done()
	log.Info("received signal, exiting")

	return nil
}
