package service

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/config"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/httpapi"
	"github.com/pood1e/wa-app/internal/waapp/management"
	"github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/rpc"
	"github.com/pood1e/wa-app/internal/waapp/runtime"
	"github.com/pood1e/wa-app/internal/waapp/store"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func RunRuntime(ctx context.Context, cfg config.Runtime) error {
	runtime, cleanup, err := newRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()
	application := runtime.Application
	server, err := rpc.NewServer(cfg.ServiceToken)
	if err != nil {
		return err
	}
	rpc.RegisterRuntime(server, application)
	adding, err := rpc.Dial(cfg.AccountAddTarget, cfg.ServiceToken)
	if err != nil {
		return err
	}
	defer adding.Close()
	group, runCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return rpc.Serve(runCtx, cfg.GRPCListenAddr, server) })
	group.Go(func() error { return application.RunLongConnections(runCtx) })
	group.Go(func() error {
		return httpapi.Serve(runCtx, httpapi.Config{ListenAddr: cfg.DashboardHTTPAddr, StaticDir: cfg.DashboardStaticDir, Administrator: runtime.Administrator, Notifications: runtime.Notifications, SetupLoopbackProxy: cfg.SetupLoopbackProxy}, application, waappv1.NewWaAccountAddServiceClient(adding))
	})
	group.Go(func() error { return runtime.Notifications.Run(runCtx) })
	return group.Wait()
}

type runtimeApplication struct {
	Application   *account.Application
	Administrator *management.Administrator
	Notifications *management.Notifications
}

func newRuntime(ctx context.Context, cfg config.Runtime) (*runtimeApplication, func(), error) {
	repository, err := openRepository(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}

	ephemeral, err := openEphemeralState(ctx, cfg)
	if err != nil {
		repository.Close()
		return nil, nil, err
	}
	protocol, err := communication.New(repository, cfg.CommonProxy, nil, nil, communication.WithProxyResolver(repository))
	if err != nil {
		_ = ephemeral.Close()
		repository.Close()
		return nil, nil, err
	}
	factory := func(ctx context.Context, input wacore.EngineMessageInput) (wacore.Communication, error) {
		return communication.NewConnection(protocol, communication.ConnectionOptions{Context: ctx, Input: input}), nil
	}
	application := account.New(repository, ephemeral, protocol, factory, nil, nil)
	cleanup := func() {
		protocol.CloseIdleConnections()
		_ = ephemeral.Close()
		repository.Close()
	}
	administrator, err := management.NewAdministrator(repository.DashboardRepository())
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	notifications, err := management.NewNotifications(ctx, repository.DashboardRepository())
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	application.SetMessageNotifier(notifications.Enqueue)
	return &runtimeApplication{Application: application, Administrator: administrator, Notifications: notifications}, cleanup, nil
}

type repository interface {
	account.Repository
	communication.StateRepository
	communication.ProxyResolver
	DashboardRepository() management.Repository
	Close()
}

func openRepository(ctx context.Context, cfg config.Runtime) (repository, error) {
	switch cfg.Store {
	case "sqlite":
		return store.NewSQLiteStoreFile(ctx, cfg.SQLiteFile)
	case "postgres":
		return store.NewPostgresStore(ctx, cfg.PGDSN)
	default:
		return nil, fmt.Errorf("WA_APP_STORE must be postgres or sqlite")
	}
}

type ephemeralState interface {
	account.EphemeralState
	Close() error
}

func openEphemeralState(ctx context.Context, cfg config.Runtime) (ephemeralState, error) {
	switch cfg.EphemeralStore {
	case "memory":
		return runtime.NewMemoryRuntime(), nil
	case "valkey":
		return runtime.NewValkeyRuntime(ctx, cfg.ValkeyURL)
	default:
		return nil, fmt.Errorf("WA_APP_EPHEMERAL_STORE must be memory or valkey")
	}
}
