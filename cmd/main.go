package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/blockchain-saas/payment-gateway/internal/app"
	"github.com/blockchain-saas/payment-gateway/internal/config"
	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/primary/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(".env")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load configuration: %v\n", err)
		return 1
	}

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "version" {
		fmt.Println(cfg.Version)
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start application: %v\n", err)
		return 1
	}

	if len(args) > 0 && args[0] != "serve" {
		commands := cli.New(cfg, application.Repositories, application.Merchants, application.Payments, application.Logger)
		code := commands.Run(ctx, args)
		_ = application.Close()
		return code
	}

	if err := application.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "application stopped with error: %v\n", err)
		_ = application.Close()
		return 1
	}
	if err := application.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to release resources: %v\n", err)
		return 1
	}
	return 0
}
