package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/blockchain-saas/payment-gateway/internal/config"
	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/repositories"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
	"github.com/blockchain-saas/payment-gateway/internal/shared/utils"
	"github.com/google/uuid"
)

type Commands struct {
	Config       *config.Config
	Repositories ports.Repositories
	Merchants    *services.MerchantService
	Payments     *services.PaymentService
	Logger       logger.Logger
}

func New(cfg *config.Config, repos ports.Repositories, merchants *services.MerchantService, payments *services.PaymentService, log logger.Logger) *Commands {
	if log == nil {
		log = logger.NopLogger{}
	}
	return &Commands{Config: cfg, Repositories: repos, Merchants: merchants, Payments: payments, Logger: log}
}

func (c *Commands) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		c.usage()
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Println(c.Config.Version)
		return 0
	case "migrate":
		return c.migrate(ctx)
	case "merchant":
		return c.merchant(ctx, args[1:])
	case "payment":
		return c.payment(ctx, args[1:])
	case "help", "-h", "--help":
		c.usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		c.usage()
		return 2
	}
}

func (c *Commands) usage() {
	fmt.Fprintln(os.Stderr, `Blockchain Payment Gateway CLI

Usage:
  payment-gateway serve
  payment-gateway migrate
  payment-gateway version
  payment-gateway merchant create --name NAME --email EMAIL [--webhook URL]
  payment-gateway merchant list [--limit N]
  payment-gateway payment list [--merchant EMAIL|UUID] [--status STATUS] [--limit N]
  payment-gateway payment get --id UUID`)
}

func (c *Commands) migrate(ctx context.Context) int {
	bundle, err := repositories.NewRepositoryFactory(ctx, repositories.Options{
		Driver:          c.Config.Database.Driver,
		DSN:             c.Config.Database.URL,
		MaxOpenConns:    c.Config.Database.MaxOpenConns,
		MaxIdleConns:    c.Config.Database.MaxIdleConns,
		ConnMaxLifetime: c.Config.Database.ConnMaxLifetime,
		AutoMigrate:     true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration failed: %v\n", err)
		return 1
	}
	defer bundle.Close()
	fmt.Println("database schema is up to date")
	return 0
}

func (c *Commands) merchant(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "merchant command requires a subcommand: create|list")
		return 2
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("merchant create", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		name := fs.String("name", "", "merchant display name")
		email := fs.String("email", "", "merchant email")
		webhook := fs.String("webhook", "", "merchant webhook url")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		merchant, apiKey, err := c.Merchants.Register(ctx, services.RegisterMerchantInput{
			Name: *name, Email: *email, WebhookURL: *webhook, Actor: "cli", UserAgent: "cli",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create merchant: %v\n", err)
			return 1
		}
		return printJSON(map[string]interface{}{
			"id": merchant.ID.String(), "name": merchant.Name, "email": merchant.Email,
			"api_key": apiKey, "webhook_secret": merchant.WebhookSecret,
		})
	case "list":
		fs := flag.NewFlagSet("merchant list", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		limit := fs.Int("limit", 50, "maximum number of merchants")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		merchants, total, err := c.Repositories.Merchants.List(ctx, ports.Pagination{Limit: *limit})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to list merchants: %v\n", err)
			return 1
		}
		return printJSON(map[string]interface{}{"total": total, "merchants": merchants})
	default:
		fmt.Fprintf(os.Stderr, "unknown merchant subcommand: %s\n", args[0])
		return 2
	}
}

func (c *Commands) payment(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "payment command requires a subcommand: list|get")
		return 2
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("payment list", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		merchantRef := fs.String("merchant", "", "merchant email or id")
		statuses := fs.String("status", "", "comma separated statuses")
		limit := fs.Int("limit", 50, "maximum number of payment intents")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		filter := ports.PaymentFilter{Pagination: ports.Pagination{Limit: *limit}}
		if *merchantRef != "" {
			merchantID, err := c.resolveMerchant(ctx, *merchantRef)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				return 1
			}
			filter.MerchantID = merchantID
		}
		for _, status := range utils.SplitCSV(*statuses) {
			filter.Statuses = append(filter.Statuses, entities.PaymentIntentStatus(strings.ToLower(status)))
		}
		payments, total, err := c.Payments.List(ctx, filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to list payment intents: %v\n", err)
			return 1
		}
		return printJSON(map[string]interface{}{"total": total, "payment_intents": payments})
	case "get":
		fs := flag.NewFlagSet("payment get", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		rawID := fs.String("id", "", "payment intent id")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		paymentID, err := uuid.Parse(*rawID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid payment intent id")
			return 2
		}
		payment, err := c.Payments.Get(ctx, uuid.Nil, paymentID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get payment intent: %v\n", err)
			return 1
		}
		return printJSON(payment)
	default:
		fmt.Fprintf(os.Stderr, "unknown payment subcommand: %s\n", args[0])
		return 2
	}
}

func (c *Commands) resolveMerchant(ctx context.Context, reference string) (uuid.UUID, error) {
	if id, err := uuid.Parse(reference); err == nil {
		return id, nil
	}
	merchant, err := c.Repositories.Merchants.GetByEmail(ctx, strings.ToLower(strings.TrimSpace(reference)))
	if err != nil {
		return uuid.Nil, fmt.Errorf("merchant %q not found", reference)
	}
	return merchant.ID, nil
}

func printJSON(value interface{}) int {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode output: %v\n", err)
		return 1
	}
	return 0
}
