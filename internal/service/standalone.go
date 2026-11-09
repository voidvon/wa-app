package service

import (
	"context"
	"io/fs"
	"log"
	"net"

	"golang.org/x/sync/errgroup"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/config"
	"github.com/pood1e/wa-app/internal/waapp/httpapi"
	"github.com/pood1e/wa-app/internal/waapp/rpc"
)

// RunStandalone serves both applications on one authenticated RPC endpoint.
func RunStandalone(ctx context.Context, cfg config.Standalone, assets fs.FS) error {
	listener, err := net.Listen("tcp", cfg.GRPCListenAddr)
	if err != nil {
		return err
	}
	defer listener.Close()
	return serveStandalone(ctx, cfg, assets, listener)
}

func serveStandalone(ctx context.Context, cfg config.Standalone, assets fs.FS, listener net.Listener) error {
	group, runCtx := errgroup.WithContext(ctx)
	runtime, cleanup, err := newRuntime(runCtx, cfg.Runtime)
	if err != nil {
		return err
	}
	defer cleanup()
	application := runtime.Application
	server, err := rpc.NewServer(cfg.ServiceToken)
	if err != nil {
		return err
	}
	defer server.Stop()
	connection, err := rpc.Dial(localTarget(listener.Addr()), cfg.ServiceToken)
	if err != nil {
		return err
	}
	defer connection.Close()
	adding, closeWorkers, err := newAccountAdd(runCtx, config.AccountAdd{
		CommonProxy: cfg.CommonProxy, IOSSignerTarget: cfg.IOSSignerTarget, AndroidSignerTarget: cfg.AndroidSignerTarget,
	}, connection)
	if err != nil {
		return err
	}
	defer closeWorkers()
	rpc.RegisterRuntime(server, application)
	rpc.RegisterAccountAdd(server, adding)
	log.Printf("wa-app gRPC listening on %s", listener.Addr())
	group.Go(func() error { return rpc.ServeListener(runCtx, listener, server) })
	group.Go(func() error { return application.RunLongConnections(runCtx) })
	group.Go(func() error { return adding.Run(runCtx) })
	group.Go(func() error {
		return httpapi.Serve(runCtx, httpapi.Config{
			ListenAddr: cfg.DashboardHTTPAddr, StaticDir: cfg.DashboardStaticDir, StaticFS: assets, Administrator: runtime.Administrator, Notifications: runtime.Notifications, SetupLoopbackProxy: cfg.SetupLoopbackProxy,
		}, application, waappv1.NewWaAccountAddServiceClient(connection))
	})
	group.Go(func() error { return runtime.Notifications.Run(runCtx) })
	return group.Wait()
}

func localTarget(address net.Addr) string {
	if tcp, ok := address.(*net.TCPAddr); ok && tcp.IP.IsUnspecified() {
		ip := net.IPv6loopback
		if tcp.IP.To4() != nil {
			ip = net.IPv4(127, 0, 0, 1)
		}
		return (&net.TCPAddr{IP: ip, Port: tcp.Port}).String()
	}
	return address.String()
}
