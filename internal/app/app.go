package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/config"
	httpadapter "github.com/blockchain-saas/payment-gateway/internal/core/adapters/primary/http"
	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/blockchain"
	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/cache"
	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/exchanges"
	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/notifications"
	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/queue"
	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/repositories"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
	"github.com/blockchain-saas/payment-gateway/internal/shared/monitoring"
	"github.com/blockchain-saas/payment-gateway/internal/workers"
)

type App struct {
	Config *config.Config

	Logger       logger.Logger
	Metrics      *monitoring.Registry
	Health       *monitoring.HealthChecker
	Repositories ports.Repositories
	Cache        ports.Cache
	Queue        ports.Queue
	Blockchain   *blockchain.Registry

	Merchants     *services.MerchantService
	Payments      *services.PaymentService
	Escrows       *services.EscrowService
	Webhooks      *services.WebhookService
	Notifications *services.NotificationService
	Exchange      *services.ExchangeService

	HTTPServer *http.Server

	closers      []func() error
	workerCancel context.CancelFunc
	workerWG     sync.WaitGroup
}

func New(ctx context.Context, cfg *config.Config) (*App, error) {
	log := logger.New(cfg.Observability.LogLevel, cfg.Observability.LogFormat)

	repositoryBundle, err := repositories.NewRepositoryFactory(ctx, repositories.Options{
		Driver:          cfg.Database.Driver,
		DSN:             cfg.Database.URL,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
		AutoMigrate:     cfg.Database.AutoMigrate,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize repositories: %w", err)
	}
	repos := repositoryBundle.Repositories

	cacheStore := cache.NewCache(cache.Options{
		Driver: cfg.Cache.Driver, Addr: cfg.Cache.Addr,
		Password: cfg.Cache.Password, DB: cfg.Cache.DB,
	})
	eventQueue := queue.NewMemoryQueue(cfg.Queue.Workers, cfg.Queue.Buffer)

	registry := blockchain.NewRegistry(blockchain.Options{
		Mock:   cfg.Blockchain.Mock,
		Chains: blockchainChains(cfg),
	})

	exchangeProvider := exchanges.NewExchange(exchanges.Options{
		Provider: cfg.Exchange.Provider, BinanceURL: cfg.Exchange.BinanceURL,
		CryptoCompareURL: cfg.Exchange.CryptoCompareURL, Timeout: cfg.Exchange.HTTPTimeout,
	})
	exchangeService := services.NewExchangeService(exchangeProvider, cacheStore, cfg.Exchange.CacheTTL, nil, log)

	notifiers := notifications.NewNotifiers(notifications.Options{
		Email: notifications.EmailOptions{
			Enabled: cfg.Notifications.Email.Enabled, Host: cfg.Notifications.Email.Host,
			Port: cfg.Notifications.Email.Port, Username: cfg.Notifications.Email.Username,
			Password: cfg.Notifications.Email.Password, From: cfg.Notifications.Email.From,
		},
		SMS: notifications.SMSOptions{
			Enabled: cfg.Notifications.SMS.Enabled, Endpoint: cfg.Notifications.SMS.Endpoint,
			Token: cfg.Notifications.SMS.Token, From: cfg.Notifications.SMS.From,
		},
		Webhook: notifications.WebhookOptions{
			Timeout:         cfg.Notifications.Webhook.Timeout,
			SignatureHeader: cfg.Notifications.Webhook.SignatureHeader,
			UserAgent:       cfg.Notifications.Webhook.UserAgent,
		},
	})

	validation := services.NewValidationService()
	fraud := services.NewFraudService(repos.Payments, services.FraudOptions{
		MaxAmountUSD:         cfg.Fraud.MaxAmountUSD,
		VelocityWindow:       cfg.Fraud.VelocityWindow,
		MaxPaymentsPerWindow: cfg.Fraud.MaxPaymentsPerWindow,
		RiskThreshold:        cfg.Fraud.RiskThreshold,
		BlacklistedAddresses: cfg.Fraud.BlacklistedAddresses,
	}, nil)

	events := services.NewQueueEventPublisher(eventQueue, services.EventsTopic)

	merchantService := services.NewMerchantService(
		repos.Merchants, repos.Audit, validation, nil, log, cfg.APIKeyEnv,
		services.TokenOptions{
			Secret: cfg.JWT.Secret, Issuer: cfg.JWT.Issuer,
			Audience: cfg.JWT.Audience, TTL: cfg.JWT.TTL,
		},
	)
	paymentService := services.NewPaymentService(
		repos.Merchants, repos.Payments, repos.Transactions, repos.Audit,
		registry, exchangeService, validation, fraud, events, nil, log,
		services.PaymentServiceOptions{DefaultExpiry: 15 * time.Minute, MaxExpiry: 24 * time.Hour},
	)
	webhookService := services.NewWebhookService(
		repos.Merchants, repos.Webhooks, notifiers[ports.ChannelWebhook], nil, log,
		services.WebhookServiceOptions{MaxRetries: cfg.Notifications.Webhook.MaxRetries},
	)
	notificationService := services.NewNotificationService(repos.Merchants, notifiers, log)
	escrowService := services.NewEscrowService(
		repos.Escrows, repos.Payments, repos.Audit, registry, events, nil, log,
		services.EscrowServiceOptions{
			DefaultReleaseDelay: cfg.Escrow.DefaultReleaseDelay,
			MaxDuration:         cfg.Escrow.MaxDuration,
			AllowForceRelease:   cfg.Escrow.AllowForceRelease,
		},
	)

	metrics := monitoring.NewRegistry()
	health := monitoring.NewHealthChecker(cfg.Version)
	health.AddCheck(monitoring.Check{Name: "database", Fn: repositoryBundle.Ping})
	health.AddCheck(monitoring.Check{Name: "cache", Fn: cacheStore.Ping})
	health.AddCheck(monitoring.Check{Name: "exchange", Fn: exchangeService.Health})
	health.AddCheck(monitoring.Check{Name: "blockchain", Fn: func(ctx context.Context) error {
		for name, err := range registry.Health(ctx) {
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		return nil
	}})

	handler := httpadapter.NewHandler(httpadapter.HandlerDeps{
		Merchants: merchantService, Payments: paymentService, Escrows: escrowService,
		Webhooks: webhookService, Exchange: exchangeService, Registry: registry,
		Audit: repos.Audit, Health: health, Metrics: metrics,
		InboundWebhookSecret: cfg.Server.InboundWebhookSecret, Logger: log,
	})
	middleware := httpadapter.NewMiddleware(httpadapter.MiddlewareOptions{
		Auth: merchantService, AdminKey: cfg.Server.AdminAPIKey,
		RateLimitRPS: cfg.Server.RateLimitRPS, RateLimitBurst: cfg.Server.RateLimitBurst,
		CORSOrigins: cfg.Server.CORSOrigins, MaxBodyBytes: cfg.Server.MaxBodyBytes,
		Metrics: metrics, Logger: log, TraceEnabled: cfg.Observability.TracingEnabled,
	})
	server := httpadapter.NewServer(handler, middleware, "web")

	app := &App{
		Config: cfg, Logger: log, Metrics: metrics, Health: health,
		Repositories: repos, Cache: cacheStore, Queue: eventQueue, Blockchain: registry,
		Merchants: merchantService, Payments: paymentService, Escrows: escrowService,
		Webhooks: webhookService, Notifications: notificationService, Exchange: exchangeService,
		HTTPServer: &http.Server{
			Addr:         ":" + cfg.Server.Port,
			Handler:      server.Handler(),
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
			IdleTimeout:  cfg.Server.IdleTimeout,
		},
	}
	app.closers = append(app.closers, repositoryBundle.Close, cacheStore.Close, eventQueue.Close)
	return app, nil
}

func blockchainChains(cfg *config.Config) map[types.BlockchainType]blockchain.AdapterOptions {
	options := make(map[types.BlockchainType]blockchain.AdapterOptions)
	for chain, chainConfig := range cfg.Chains() {
		options[chain] = blockchain.AdapterOptions{
			Enabled:               chainConfig.Enabled,
			RPCURL:                chainConfig.RPCURL,
			RPCUser:               chainConfig.RPCUser,
			RPCPassword:           chainConfig.RPCPassword,
			APIKey:                chainConfig.APIKey,
			DepositAddress:        chainConfig.DepositAddress,
			Network:               chainConfig.Network,
			RequiredConfirmations: chainConfig.RequiredConfirmations,
			LookbackBlocks:        chainConfig.LookbackBlocks,
			Timeout:               cfg.Blockchain.HTTPTimeout,
			FeeMultiplier:         chainConfig.FeeMultiplier,
		}
	}
	return options
}

func (a *App) StartWorkers(ctx context.Context) {
	workerCtx, cancel := context.WithCancel(ctx)
	a.workerCancel = cancel

	callback := workers.NewCallbackWorker(a.Queue, a.Webhooks, a.Notifications, services.EventsTopic, a.Logger)
	if err := callback.Start(workerCtx); err != nil {
		a.Logger.Error(workerCtx, "failed to start callback worker", "error", err)
	}

	expiration := workers.NewExpirationWorker(a.Payments, 30*time.Second, a.Logger)
	confirmation := workers.NewConfirmationWorker(a.Payments, a.Config.Blockchain.ConfirmationInterval, a.Logger)
	watcher := workers.NewAddressWatcher(a.Repositories.Payments, a.Blockchain, a.Payments, a.Config.Blockchain.WatchInterval, a.Logger)
	retries := workers.NewWebhookRetryWorker(a.Webhooks, 30*time.Second, a.Logger)
	escrows := workers.NewEscrowWorker(a.Escrows, time.Minute, a.Logger)

	for _, run := range []func(context.Context){
		expiration.Run, confirmation.Run, watcher.Run, retries.Run, escrows.Run,
	} {
		a.workerWG.Add(1)
		go func(run func(context.Context)) {
			defer a.workerWG.Done()
			run(workerCtx)
		}(run)
	}
}

func (a *App) Run(ctx context.Context) error {
	a.StartWorkers(ctx)
	defer a.stopWorkers()

	listener, err := net.Listen("tcp", a.HTTPServer.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", a.HTTPServer.Addr, err)
	}

	errCh := make(chan error, 1)
	go func() {
		a.Logger.Info(ctx, "http server listening", "addr", a.HTTPServer.Addr, "env", a.Config.Environment)
		if serveErr := a.HTTPServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.Config.Server.ShutdownTimeout)
		defer cancel()
		if err := a.HTTPServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown http server: %w", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}

func (a *App) stopWorkers() {
	if a.workerCancel != nil {
		a.workerCancel()
	}
	a.workerWG.Wait()
}

func (a *App) Close() error {
	a.stopWorkers()
	var firstErr error
	for _, closer := range a.closers {
		if closer == nil {
			continue
		}
		if err := closer(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	a.closers = nil
	return firstErr
}
