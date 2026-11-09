package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caarlos0/env/v11"
)

type Runtime struct {
	ServiceToken       string `env:"WA_SERVICE_TOKEN"`
	GRPCListenAddr     string `env:"WA_APP_LISTEN_ADDR" envDefault:":50091"`
	DashboardHTTPAddr  string `env:"WA_APP_DASHBOARD_HTTP_ADDR" envDefault:":8080"`
	DashboardStaticDir string `env:"WA_APP_DASHBOARD_STATIC_DIR" envDefault:"/app/dashboard/wa"`
	SetupLoopbackProxy bool   `env:"WA_APP_SETUP_LOOPBACK_PROXY"`
	Store              string `env:"WA_APP_STORE"`
	PGDSN              string `env:"WA_APP_PG_DSN"`
	SQLiteFile         string `env:"WA_APP_SQLITE_FILE"`
	EphemeralStore     string `env:"WA_APP_EPHEMERAL_STORE" envDefault:"memory"`
	ValkeyURL          string `env:"WA_APP_VALKEY_URL"`
	AccountAddTarget   string `env:"WA_ACCOUNT_ADD_TARGET"`
	CommonProxy        string `env:"WA_COMMON_PROXY"`
}

type AccountAdd struct {
	ServiceToken        string `env:"WA_SERVICE_TOKEN,required"`
	GRPCListenAddr      string `env:"WA_ACCOUNT_ADD_LISTEN_ADDR" envDefault:":50092"`
	RuntimeTarget       string `env:"WA_ACCOUNT_RUNTIME_TARGET,required"`
	CommonProxy         string `env:"WA_COMMON_PROXY"`
	IOSSignerTarget     string `env:"WA_ACCOUNT_ADD_IOS_SIGNER_TARGET"`
	AndroidSignerTarget string `env:"WA_ACCOUNT_ADD_ANDROID_SIGNER_TARGET"`
}

// Standalone runs both applications with a shared local RPC transport.
type Standalone struct {
	Runtime
	IOSSignerTarget     string `env:"WA_ACCOUNT_ADD_IOS_SIGNER_TARGET"`
	AndroidSignerTarget string `env:"WA_ACCOUNT_ADD_ANDROID_SIGNER_TARGET"`
}

func LoadStandalone() (Standalone, error) {
	var cfg Standalone
	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("required standalone configuration is missing or invalid")
	}
	if value, set := os.LookupEnv("WA_APP_LISTEN_ADDR"); set {
		cfg.GRPCListenAddr = value
	} else {
		cfg.GRPCListenAddr = "127.0.0.1:50091"
	}
	if value, set := os.LookupEnv("WA_APP_DASHBOARD_HTTP_ADDR"); set {
		cfg.DashboardHTTPAddr = value
	} else {
		cfg.DashboardHTTPAddr = "127.0.0.1:8080"
	}
	if value, set := os.LookupEnv("WA_APP_DASHBOARD_STATIC_DIR"); set {
		cfg.DashboardStaticDir = value
	} else {
		cfg.DashboardStaticDir = ""
	}
	if cfg.Store == "" {
		if strings.TrimSpace(cfg.PGDSN) != "" {
			return cfg, fmt.Errorf("WA_APP_STORE must explicitly select postgres when WA_APP_PG_DSN is set")
		}
		cfg.Store = "sqlite"
	}
	if cfg.Store == "sqlite" && cfg.SQLiteFile == "" {
		cfg.SQLiteFile = filepath.Join("data", "wa-app.sqlite3")
	}
	if cfg.ServiceToken == "" {
		var secret [32]byte
		if _, err := rand.Read(secret[:]); err != nil {
			return cfg, fmt.Errorf("generate internal service token failed")
		}
		cfg.ServiceToken = hex.EncodeToString(secret[:])
	}
	if err := validateRuntime(cfg.Runtime); err != nil {
		return cfg, err
	}
	if strings.TrimSpace(cfg.GRPCListenAddr) == "" {
		return cfg, fmt.Errorf("WA_APP_LISTEN_ADDR is required")
	}
	return cfg, nil
}

func LoadRuntime() (Runtime, error) {
	var cfg Runtime
	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("required runtime configuration is missing or invalid")
	}
	if err := validateRuntime(cfg); err != nil {
		return cfg, err
	}
	if strings.TrimSpace(cfg.AccountAddTarget) == "" {
		return cfg, fmt.Errorf("account adding service target is required")
	}
	return cfg, nil
}

func validateRuntime(cfg Runtime) error {
	switch cfg.Store {
	case "postgres":
		if strings.TrimSpace(cfg.PGDSN) == "" {
			return fmt.Errorf("WA_APP_PG_DSN is required for postgres")
		}
	case "sqlite":
		if strings.TrimSpace(cfg.SQLiteFile) == "" {
			return fmt.Errorf("WA_APP_SQLITE_FILE is required for sqlite")
		}
	default:
		return fmt.Errorf("WA_APP_STORE must be postgres or sqlite")
	}
	switch cfg.EphemeralStore {
	case "memory":
	case "valkey":
		if strings.TrimSpace(cfg.ValkeyURL) == "" {
			return fmt.Errorf("WA_APP_VALKEY_URL is required for valkey")
		}
	default:
		return fmt.Errorf("WA_APP_EPHEMERAL_STORE must be memory or valkey")
	}
	if strings.TrimSpace(cfg.ServiceToken) == "" {
		return fmt.Errorf("service token is required")
	}
	return nil
}

func LoadAccountAdd() (AccountAdd, error) {
	var cfg AccountAdd
	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("required account adding configuration is missing or invalid")
	}
	if cfg.ServiceToken == "" || cfg.RuntimeTarget == "" {
		return cfg, fmt.Errorf("service token and account runtime target are required")
	}
	return cfg, nil
}
