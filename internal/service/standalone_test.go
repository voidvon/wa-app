package service

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/config"
	"github.com/pood1e/wa-app/internal/waapp/rpc"
)

func TestStandaloneServicesAndShutdown(t *testing.T) {
	cfg := config.Standalone{Runtime: config.Runtime{
		ServiceToken: "synthetic-service-token", Store: "sqlite", EphemeralStore: "memory",
		SQLiteFile: filepath.Join(t.TempDir(), "accounts.sqlite3"),
	}}
	for range 2 {
		// Restart against the same database to check ownership and resource cleanup.
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			defer listener.Close()
			done <- serveStandalone(ctx, cfg, nil, listener)
		}()
		t.Cleanup(cancel)
		requestCtx, stop := context.WithTimeout(t.Context(), 10*time.Second)
		connection, err := rpc.Dial(listener.Addr().String(), cfg.ServiceToken)
		if err != nil {
			stop()
			cancel()
			t.Fatal(err)
		}
		accounts, err := waappv1.NewWaProfileServiceClient(connection).ListWAAccounts(requestCtx, &waappv1.ListWAAccountsRequest{})
		if err != nil || accounts.GetError() != nil || len(accounts.GetAccounts()) != 0 {
			t.Errorf("runtime RPC was not available: %v", err)
		}
		flow, err := waappv1.NewWaAccountAddServiceClient(connection).GetAccountAdd(requestCtx, &waappv1.AccountAddRequest{FlowId: "synthetic-missing"})
		if err != nil || flow.GetError() == nil || flow.GetError().GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_EXPIRED {
			t.Errorf("adding RPC was not available on the same endpoint: %v", err)
		}
		unauthorized, err := rpc.Dial(listener.Addr().String(), "synthetic-wrong-token")
		if err != nil {
			t.Error(err)
		} else {
			_, err = waappv1.NewWaAccountAddServiceClient(unauthorized).GetAccountAdd(requestCtx, &waappv1.AccountAddRequest{})
			if status.Code(err) != codes.Unauthenticated {
				t.Error("combined adding service did not enforce RPC authentication")
			}
			_ = unauthorized.Close()
		}
		_ = connection.Close()
		stop()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("standalone did not stop both services")
		}
	}
}

func TestLocalTarget(t *testing.T) {
	for _, tc := range []struct{ address, target string }{
		{"0.0.0.0:50091", "127.0.0.1:50091"},
		{"[::]:50091", "[::1]:50091"},
		{"127.0.0.1:50091", "127.0.0.1:50091"},
	} {
		address, err := net.ResolveTCPAddr("tcp", tc.address)
		if err != nil {
			t.Fatal(err)
		}
		if localTarget(address) != tc.target {
			t.Fatalf("wildcard listener %s is not reachable internally", tc.address)
		}
	}
}
