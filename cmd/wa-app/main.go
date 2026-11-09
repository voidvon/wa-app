package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/pood1e/wa-app/internal/config"
	"github.com/pood1e/wa-app/internal/migrate"
	"github.com/pood1e/wa-app/internal/service"
	"github.com/pood1e/wa-app/internal/waapp/rpc"
	dashboard "github.com/pood1e/wa-app/webui"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	if len(args) > 1 {
		return fmt.Errorf("expected a single command; use wa-app help")
	}
	switch command {
	case "help", "-h", "--help":
		_, err := fmt.Fprintln(output, "Usage: wa-app [serve|health|migrate|version|help]\n\nserve    Start Dashboard and both account services (default)\nhealth   Check the local gRPC service\nmigrate  Convert an existing legacy database while the service is stopped\nversion  Print the build version\n\nserve defaults to SQLite at ./data/wa-app.sqlite3 and a random internal service token.\nSet WA_SERVICE_TOKEN for external gRPC clients.\nmigrate requires explicit database configuration. See README.md.")
		return err
	case "version", "--version":
		_, err := fmt.Fprintln(output, version)
		return err
	case "migrate":
		if err := migrate.Run(ctx); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, "account storage migration completed")
		return err
	case "health":
		address := os.Getenv("WA_APP_LISTEN_ADDR")
		if address == "" {
			address = "127.0.0.1:50091"
		}
		return rpc.CheckHealth(ctx, address)
	case "serve":
		cfg, err := config.LoadStandalone()
		if err != nil {
			return err
		}
		var assets fs.FS
		if cfg.DashboardHTTPAddr != "" && cfg.DashboardStaticDir == "" {
			assets, err = dashboard.Files()
			if err != nil {
				return err
			}
		}
		return service.RunStandalone(ctx, cfg, assets)
	default:
		return fmt.Errorf("unknown command; use wa-app help")
	}
}
