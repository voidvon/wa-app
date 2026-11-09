package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStandaloneConfiguration(t *testing.T) {
	t.Setenv("WA_SERVICE_TOKEN", "synthetic-service-token")
	t.Setenv("WA_APP_STORE", "sqlite")
	t.Setenv("WA_APP_SQLITE_FILE", t.TempDir()+"/accounts.sqlite3")
	t.Setenv("WA_APP_EPHEMERAL_STORE", "memory")
	t.Setenv("WA_ACCOUNT_ADD_TARGET", "")
	t.Setenv("WA_ACCOUNT_RUNTIME_TARGET", "")
	t.Setenv("WA_ACCOUNT_ADD_IOS_SIGNER_TARGET", "unix:///synthetic-ios.sock")
	t.Setenv("WA_ACCOUNT_ADD_ANDROID_SIGNER_TARGET", "unix:///synthetic-android.sock")
	for _, name := range []string{"WA_APP_LISTEN_ADDR", "WA_APP_DASHBOARD_HTTP_ADDR", "WA_APP_DASHBOARD_STATIC_DIR"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := LoadStandalone()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GRPCListenAddr != "127.0.0.1:50091" || cfg.DashboardHTTPAddr != "127.0.0.1:8080" || cfg.DashboardStaticDir != "" {
		t.Fatal("standalone defaults require container paths or nonlocal listeners")
	}
	if cfg.IOSSignerTarget != "unix:///synthetic-ios.sock" || cfg.AndroidSignerTarget != "unix:///synthetic-android.sock" {
		t.Fatal("optional worker configuration was lost")
	}
	if _, err := LoadRuntime(); err == nil {
		t.Fatal("separate runtime accepted a missing adding service target")
	}
	t.Setenv("WA_APP_DASHBOARD_HTTP_ADDR", "")
	t.Setenv("WA_APP_DASHBOARD_STATIC_DIR", "")
	t.Setenv("WA_APP_LISTEN_ADDR", "127.0.0.1:50991")
	cfg, err = LoadStandalone()
	if err != nil || cfg.DashboardHTTPAddr != "" || cfg.DashboardStaticDir != "" || cfg.GRPCListenAddr != "127.0.0.1:50991" {
		t.Fatal("explicit listener or embedded asset configuration was ignored")
	}
	t.Setenv("WA_APP_STORE", "")
	t.Setenv("WA_APP_PG_DSN", "")
	cfg, err = LoadStandalone()
	if err != nil || cfg.Store != "sqlite" {
		t.Fatal("standalone did not default to SQLite")
	}
	t.Setenv("WA_APP_STORE", "sqlite")
	t.Setenv("WA_APP_SQLITE_FILE", "")
	cfg, err = LoadStandalone()
	if err != nil || cfg.SQLiteFile != filepath.Join("data", "wa-app.sqlite3") {
		t.Fatal("standalone did not choose its local SQLite file")
	}
}

func TestStandaloneGeneratedTokenAndStorageSelection(t *testing.T) {
	t.Setenv("WA_SERVICE_TOKEN", "")
	t.Setenv("WA_APP_STORE", "")
	t.Setenv("WA_APP_SQLITE_FILE", "")
	t.Setenv("WA_APP_PG_DSN", "")
	t.Setenv("WA_APP_EPHEMERAL_STORE", "memory")
	t.Setenv("WA_APP_LISTEN_ADDR", "127.0.0.1:50091")
	first, err := LoadStandalone()
	if err != nil {
		t.Fatal("standalone required a manually configured token")
	}
	second, err := LoadStandalone()
	if err != nil || len(first.ServiceToken) < 32 || first.ServiceToken == second.ServiceToken {
		t.Fatal("internal service tokens are empty, short or reused")
	}
	if os.Getenv("WA_SERVICE_TOKEN") != "" {
		t.Fatal("generated token escaped into the process environment")
	}
	t.Setenv("WA_SERVICE_TOKEN", "synthetic-fixed-token")
	cfg, err := LoadStandalone()
	if err != nil || cfg.ServiceToken != "synthetic-fixed-token" {
		t.Fatal("configured service token was replaced")
	}
	t.Setenv("WA_APP_PG_DSN", "postgres://localhost/synthetic")
	if _, err := LoadStandalone(); err == nil {
		t.Fatal("PostgreSQL configuration silently opened a local SQLite database")
	}
	t.Setenv("WA_APP_STORE", "postgres")
	cfg, err = LoadStandalone()
	if err != nil || cfg.Store != "postgres" || cfg.SQLiteFile != "" {
		t.Fatal("explicit PostgreSQL configuration was not preserved")
	}
	t.Setenv("WA_APP_PG_DSN", "")
	if _, err := LoadStandalone(); err == nil {
		t.Fatal("missing PostgreSQL configuration fell back to SQLite")
	}
	t.Setenv("WA_APP_STORE", "invalid")
	if _, err := LoadStandalone(); err == nil {
		t.Fatal("invalid storage backend fell back to SQLite")
	}
}
