package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/pood1e/wa-app/internal/migrate"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Print(err)
		os.Exit(1)
	}
	log.Print("account storage migration completed")
}

func run(ctx context.Context) error {
	return migrate.Run(ctx)
}
