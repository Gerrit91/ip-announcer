package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	api "github.com/osrg/gobgp/v3/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
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
			PeerAsn:         65000,
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
