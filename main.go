package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"

	api "github.com/osrg/gobgp/v3/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"

	"gopkg.in/yaml.v3"

	"github.com/metal-stack/metal-go/api/models"
	hammerapi "github.com/metal-stack/metal-hammer/pkg/api"
	"github.com/metal-stack/metal-lib/pkg/net"
	"github.com/metal-stack/metal-lib/pkg/pointer"
)

const (
	installYAML = "/etc/metal/install.yaml"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(log); err != nil {
		log.Error("error running application", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
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

	asn := pointer.SafeDeref(install.Networks[idx].Asn)

	log.Info("figured out peer asn", "asn", asn)

	conn, err := grpc.NewClient("127.0.0.1:179", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to bgp daemon: %w", err)
	}
	defer func() {
		err = conn.Close()
	}()

	client := api.NewGobgpApiClient(conn)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	_, err = client.AddPeer(ctx, &api.AddPeerRequest{Peer: &api.Peer{
		Conf: &api.PeerConf{
			NeighborAddress: "127.0.0.1",
			PeerAsn:         uint32(asn),
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

	nlri, err := anypb.New(&api.IPAddressPrefix{
		Prefix:    ip,
		PrefixLen: 32,
	})
	if err != nil {
		return err
	}

	_, err = client.AddPath(ctx, &api.AddPathRequest{
		Path: &api.Path{
			Nlri: nlri,
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
