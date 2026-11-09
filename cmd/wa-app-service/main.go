package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/pood1e/wa-app/internal/config"
	"github.com/pood1e/wa-app/internal/service"
	"github.com/pood1e/wa-app/internal/waapp/rpc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadRuntime()
	if err != nil {
		return err
	}
	if len(os.Args) == 2 && os.Args[1] == "health" {
		return rpc.CheckHealth(ctx, cfg.GRPCListenAddr)
	}
	return service.RunRuntime(ctx, cfg)
}
