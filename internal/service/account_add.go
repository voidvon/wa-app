package service

import (
	"context"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/config"
	addapp "github.com/pood1e/wa-app/internal/waapp/app/onboarding"
	"github.com/pood1e/wa-app/internal/waapp/protocol/attestation"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding/ios"
	"github.com/pood1e/wa-app/internal/waapp/rpc"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func RunAccountAdd(ctx context.Context, cfg config.AccountAdd) error {
	connection, err := rpc.Dial(cfg.RuntimeTarget, cfg.ServiceToken)
	if err != nil {
		return err
	}
	defer connection.Close()
	group, runCtx := errgroup.WithContext(ctx)
	application, cleanup, err := newAccountAdd(runCtx, cfg, connection)
	if err != nil {
		return err
	}
	defer cleanup()
	server, err := rpc.NewServer(cfg.ServiceToken)
	if err != nil {
		return err
	}
	rpc.RegisterAccountAdd(server, application)
	group.Go(func() error { return application.Run(runCtx) })
	group.Go(func() error { return rpc.Serve(runCtx, cfg.GRPCListenAddr, server) })
	return group.Wait()
}

func newAccountAdd(ctx context.Context, cfg config.AccountAdd, connection grpc.ClientConnInterface) (*addapp.Service, func(), error) {
	var connections []*grpc.ClientConn
	cleanup := func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	}
	ready := false
	defer func() {
		if !ready {
			cleanup()
		}
	}()
	build, err := onboarding.DefaultAndroidBuild()
	if err != nil {
		return nil, nil, err
	}
	parameters := onboarding.DefaultAndroidClient()
	iosProfile, err := ios.DefaultProfile()
	if err != nil {
		return nil, nil, err
	}
	var iosDependencies ios.Dependencies
	if cfg.IOSSignerTarget != "" {
		connection, err := attestation.Dial(cfg.IOSSignerTarget)
		if err != nil {
			return nil, nil, err
		}
		connections = append(connections, connection)
		iosDependencies.Attestation = waappv1.NewAppAttestServiceClient(connection)
		iosDependencies.Recaptcha = waappv1.NewRecaptchaServiceClient(connection)
	}
	var androidSigner waappv1.AndroidAttestationServiceClient
	if cfg.AndroidSignerTarget != "" {
		connection, err := attestation.Dial(cfg.AndroidSignerTarget)
		if err != nil {
			return nil, nil, err
		}
		connections = append(connections, connection)
		androidSigner = waappv1.NewAndroidAttestationServiceClient(connection)
	}
	factory := func(ctx context.Context, request *waappv1.BeginAccountAddRequest) (addapp.Protocol, error) {
		proxy, proxyURL, err := resolveAccountProxy(ctx, waappv1.NewWaAccountAcceptanceServiceClient(connection), request, cfg.CommonProxy)
		if err != nil {
			return nil, err
		}
		var protocol addapp.Protocol
		switch request.GetPlatform() {
		case protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID:
			protocol, err = onboarding.NewAndroidFlow(ctx, request.GetPhone(), build, parameters, proxyURL, onboarding.AndroidDependencies{Attestation: androidSigner})
		case protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS:
			protocol, err = onboarding.NewIOSFlow(ctx, request.GetPhone(), iosProfile, proxyURL, iosDependencies)
		default:
			return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "account platform is not supported", false)
		}
		if err != nil {
			return nil, err
		}
		return addapp.WithAccountProxy(protocol, proxy), nil
	}
	validate := func(ctx context.Context, material *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
		return onboarding.ValidateLogin(ctx, material, cfg.CommonProxy)
	}
	application := addapp.New(ctx, factory, validate, rpc.NewAcceptanceClient(connection), nil, nil)
	ready = true
	return application, cleanup, nil
}
