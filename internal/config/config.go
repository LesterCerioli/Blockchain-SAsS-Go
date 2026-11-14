package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/shared/utils"
)

type Config struct {
	Environment   string
	Version       string
	Server        ServerConfig
	Database      DatabaseConfig
	Cache         CacheConfig
	JWT           JWTConfig
	APIKeyEnv     string
	Blockchain    BlockchainConfig
	Exchange      ExchangeConfig
	Notifications NotificationConfig
	Queue         QueueConfig
	Fraud         FraudConfig
	Escrow        EscrowConfig
	Observability ObservabilityConfig
}

type ServerConfig struct {
	Port                 string
	ReadTimeout          time.Duration
	WriteTimeout         time.Duration
	IdleTimeout          time.Duration
	ShutdownTimeout      time.Duration
	MaxBodyBytes         int64
	CORSOrigins          []string
	RateLimitRPS         int
	RateLimitBurst       int
	GRPCEnabled          bool
	GRPCPort             string
	AdminAPIKey          string
	InboundWebhookSecret string
}

type DatabaseConfig struct {
	Driver          string
	URL             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	AutoMigrate     bool
}

type CacheConfig struct {
	Driver   string
	Addr     string
	Password string
	DB       int
	TTL      time.Duration
}

type JWTConfig struct {
	Secret   string
	Issuer   string
	Audience []string
	TTL      time.Duration
}

type ChainConfig struct {
	Enabled               bool
	RPCURL                string
	RPCUser               string
	RPCPassword           string
	WSUrl                 string
	DepositAddress        string
	RequiredConfirmations int
	LookbackBlocks        int
	APIKey                string
	Network               types.BlockchainNetwork
	FeeMultiplier         float64
}

type BlockchainConfig struct {
	Ethereum             ChainConfig
	Bitcoin              ChainConfig
	Solana               ChainConfig
	Cardano              ChainConfig
	Mock                 bool
	WatchInterval        time.Duration
	ConfirmationInterval time.Duration
	HTTPTimeout          time.Duration
}

type ExchangeConfig struct {
	Provider         string
	BinanceURL       string
	CryptoCompareURL string
	CacheTTL         time.Duration
	HTTPTimeout      time.Duration
}

type NotificationConfig struct {
	Email   EmailConfig
	SMS     SMSConfig
	Webhook WebhookConfig
}

type EmailConfig struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

type SMSConfig struct {
	Enabled  bool
	Endpoint string
	Token    string
	From     string
}

type WebhookConfig struct {
	Timeout         time.Duration
	MaxRetries      int
	SignatureHeader string
	UserAgent       string
}

type QueueConfig struct {
	Driver  string
	Workers int
	Buffer  int
}

type FraudConfig struct {
	MaxAmountUSD         float64
	VelocityWindow       time.Duration
	MaxPaymentsPerWindow int
	RiskThreshold        int
	BlacklistedAddresses []string
	BlockedCountries     []string
}

type EscrowConfig struct {
	DefaultReleaseDelay time.Duration
	MaxDuration         time.Duration
	AllowForceRelease   bool
}

type ObservabilityConfig struct {
	MetricsEnabled bool
	TracingEnabled bool
	LogLevel       string
	LogFormat      string
}

func Load(paths ...string) (*Config, error) {
	for _, path := range paths {
		if path != "" {
			_ = loadDotEnv(path)
		}
	}

	cfg := &Config{
		Environment: env("APP_ENV", "development"),
		Version:     env("APP_VERSION", "1.0.0"),
		Server: ServerConfig{
			Port:                 env("PORT", "8080"),
			ReadTimeout:          utils.ParseDurationOrDefault(env("SERVER_READ_TIMEOUT", "15s"), 15*time.Second),
			WriteTimeout:         utils.ParseDurationOrDefault(env("SERVER_WRITE_TIMEOUT", "30s"), 30*time.Second),
			IdleTimeout:          utils.ParseDurationOrDefault(env("SERVER_IDLE_TIMEOUT", "60s"), 60*time.Second),
			ShutdownTimeout:      utils.ParseDurationOrDefault(env("SERVER_SHUTDOWN_TIMEOUT", "20s"), 20*time.Second),
			MaxBodyBytes:         int64(envInt("SERVER_MAX_BODY_BYTES", 1<<20)),
			CORSOrigins:          utils.SplitCSV(env("CORS_ORIGINS", "http://localhost:3000")),
			RateLimitRPS:         envInt("RATE_LIMIT_RPS", 20),
			RateLimitBurst:       envInt("RATE_LIMIT_BURST", 40),
			GRPCEnabled:          utils.BoolFromEnv(env("GRPC_ENABLED", "false"), false),
			GRPCPort:             env("GRPC_PORT", "9090"),
			AdminAPIKey:          env("ADMIN_API_KEY", ""),
			InboundWebhookSecret: env("INBOUND_WEBHOOK_SECRET", ""),
		},
		Database: DatabaseConfig{
			Driver:          env("DATABASE_DRIVER", "memory"),
			URL:             env("DATABASE_URL", ""),
			MaxOpenConns:    envInt("DATABASE_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    envInt("DATABASE_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: utils.ParseDurationOrDefault(env("DATABASE_CONN_MAX_LIFETIME", "30m"), 30*time.Minute),
			AutoMigrate:     utils.BoolFromEnv(env("DATABASE_AUTO_MIGRATE", "true"), true),
		},
		Cache: CacheConfig{
			Driver:   env("CACHE_DRIVER", "memory"),
			Addr:     env("REDIS_ADDR", "localhost:6379"),
			Password: env("REDIS_PASSWORD", ""),
			DB:       envInt("REDIS_DB", 0),
			TTL:      utils.ParseDurationOrDefault(env("CACHE_DEFAULT_TTL", "5m"), 5*time.Minute),
		},
		JWT: JWTConfig{
			Secret:   env("JWT_SECRET", "development-only-secret-change-me"),
			Issuer:   env("JWT_ISSUER", "blockchain-payment-gateway"),
			Audience: utils.SplitCSV(env("JWT_AUDIENCE", "merchant-api")),
			TTL:      utils.ParseDurationOrDefault(env("JWT_TTL", "24h"), 24*time.Hour),
		},
		APIKeyEnv: env("API_KEY_ENV", "test"),
		Blockchain: BlockchainConfig{
			Ethereum: ChainConfig{
				Enabled:               utils.BoolFromEnv(env("ETHEREUM_ENABLED", "true"), true),
				RPCURL:                env("ETHEREUM_RPC_URL", "http://localhost:8545"),
				WSUrl:                 env("ETHEREUM_WS_URL", ""),
				DepositAddress:        env("ETHEREUM_DEPOSIT_ADDRESS", ""),
				RequiredConfirmations: envInt("ETHEREUM_CONFIRMATIONS", 12),
				LookbackBlocks:        envInt("ETHEREUM_LOOKBACK_BLOCKS", 12),
				Network:               networkFromEnv("ETHEREUM_NETWORK", types.NetworkEthereumSepolia),
				FeeMultiplier:         envFloat("ETHEREUM_FEE_MULTIPLIER", 1.1),
			},
			Bitcoin: ChainConfig{
				Enabled:               utils.BoolFromEnv(env("BITCOIN_ENABLED", "true"), true),
				RPCURL:                env("BITCOIN_RPC_URL", "http://localhost:8332"),
				RPCUser:               env("BITCOIN_RPC_USER", "bitcoin"),
				RPCPassword:           env("BITCOIN_RPC_PASSWORD", "bitcoin"),
				DepositAddress:        env("BITCOIN_DEPOSIT_ADDRESS", ""),
				RequiredConfirmations: envInt("BITCOIN_CONFIRMATIONS", 3),
				Network:               networkFromEnv("BITCOIN_NETWORK", types.NetworkBitcoinTestnet),
				FeeMultiplier:         envFloat("BITCOIN_FEE_MULTIPLIER", 1.1),
			},
			Solana: ChainConfig{
				Enabled:               utils.BoolFromEnv(env("SOLANA_ENABLED", "true"), true),
				RPCURL:                env("SOLANA_RPC_URL", "http://localhost:8899"),
				DepositAddress:        env("SOLANA_DEPOSIT_ADDRESS", ""),
				RequiredConfirmations: envInt("SOLANA_CONFIRMATIONS", 1),
				Network:               networkFromEnv("SOLANA_NETWORK", types.NetworkDevnet),
				FeeMultiplier:         envFloat("SOLANA_FEE_MULTIPLIER", 1.0),
			},
			Cardano: ChainConfig{
				Enabled:               utils.BoolFromEnv(env("CARDANO_ENABLED", "false"), false),
				RPCURL:                env("CARDANO_RPC_URL", "https://cardano-preprod.blockfrost.io/api/v0"),
				APIKey:                env("CARDANO_API_KEY", ""),
				DepositAddress:        env("CARDANO_DEPOSIT_ADDRESS", ""),
				RequiredConfirmations: envInt("CARDANO_CONFIRMATIONS", 15),
				Network:               networkFromEnv("CARDANO_NETWORK", types.NetworkTestnet),
				FeeMultiplier:         envFloat("CARDANO_FEE_MULTIPLIER", 1.0),
			},
			Mock:                 utils.BoolFromEnv(env("BLOCKCHAIN_MOCK", "false"), false),
			WatchInterval:        utils.ParseDurationOrDefault(env("BLOCKCHAIN_WATCH_INTERVAL", "15s"), 15*time.Second),
			ConfirmationInterval: utils.ParseDurationOrDefault(env("BLOCKCHAIN_CONFIRMATION_INTERVAL", "10s"), 10*time.Second),
			HTTPTimeout:          utils.ParseDurationOrDefault(env("BLOCKCHAIN_HTTP_TIMEOUT", "15s"), 15*time.Second),
		},
		Exchange: ExchangeConfig{
			Provider:         env("EXCHANGE_PROVIDER", "binance"),
			BinanceURL:       env("BINANCE_API_URL", "https://api.binance.com"),
			CryptoCompareURL: env("CRYPTOCOMPARE_API_URL", "https://min-api.cryptocompare.com"),
			CacheTTL:         utils.ParseDurationOrDefault(env("EXCHANGE_CACHE_TTL", "60s"), 60*time.Second),
			HTTPTimeout:      utils.ParseDurationOrDefault(env("EXCHANGE_HTTP_TIMEOUT", "10s"), 10*time.Second),
		},
		Notifications: NotificationConfig{
			Email: EmailConfig{
				Enabled:  utils.BoolFromEnv(env("EMAIL_ENABLED", "false"), false),
				Host:     env("SMTP_HOST", "localhost"),
				Port:     envInt("SMTP_PORT", 1025),
				Username: env("SMTP_USERNAME", ""),
				Password: env("SMTP_PASSWORD", ""),
				From:     env("EMAIL_FROM", "no-reply@example.com"),
			},
			SMS: SMSConfig{
				Enabled:  utils.BoolFromEnv(env("SMS_ENABLED", "false"), false),
				Endpoint: env("SMS_ENDPOINT", ""),
				Token:    env("SMS_TOKEN", ""),
				From:     env("SMS_FROM", ""),
			},
			Webhook: WebhookConfig{
				Timeout:         utils.ParseDurationOrDefault(env("WEBHOOK_TIMEOUT", "10s"), 10*time.Second),
				MaxRetries:      envInt("WEBHOOK_MAX_RETRIES", 5),
				SignatureHeader: env("WEBHOOK_SIGNATURE_HEADER", "X-Signature"),
				UserAgent:       env("WEBHOOK_USER_AGENT", "blockchain-payment-gateway/1.0"),
			},
		},
		Queue: QueueConfig{
			Driver:  env("QUEUE_DRIVER", "memory"),
			Workers: envInt("QUEUE_WORKERS", 4),
			Buffer:  envInt("QUEUE_BUFFER", 1024),
		},
		Fraud: FraudConfig{
			MaxAmountUSD:         envFloat("FRAUD_MAX_AMOUNT_USD", 100000),
			VelocityWindow:       utils.ParseDurationOrDefault(env("FRAUD_VELOCITY_WINDOW", "1h"), time.Hour),
			MaxPaymentsPerWindow: envInt("FRAUD_MAX_PAYMENTS_PER_WINDOW", 30),
			RiskThreshold:        envInt("FRAUD_RISK_THRESHOLD", 70),
			BlacklistedAddresses: utils.SplitCSV(env("FRAUD_BLACKLISTED_ADDRESSES", "")),
			BlockedCountries:     utils.SplitCSV(env("FRAUD_BLOCKED_COUNTRIES", "")),
		},
		Escrow: EscrowConfig{
			DefaultReleaseDelay: utils.ParseDurationOrDefault(env("ESCROW_DEFAULT_RELEASE_DELAY", "0s"), 0),
			MaxDuration:         utils.ParseDurationOrDefault(env("ESCROW_MAX_DURATION", "720h"), 720*time.Hour),
			AllowForceRelease:   utils.BoolFromEnv(env("ESCROW_ALLOW_FORCE_RELEASE", "false"), false),
		},
		Observability: ObservabilityConfig{
			MetricsEnabled: utils.BoolFromEnv(env("METRICS_ENABLED", "true"), true),
			TracingEnabled: utils.BoolFromEnv(env("TRACING_ENABLED", "true"), true),
			LogLevel:       env("LOG_LEVEL", "info"),
			LogFormat:      env("LOG_FORMAT", "json"),
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Server.Port == "" {
		return fmt.Errorf("PORT cannot be empty")
	}
	if c.Environment == "production" {
		if len(c.JWT.Secret) < 32 || strings.Contains(c.JWT.Secret, "development") {
			return fmt.Errorf("JWT_SECRET must be at least 32 characters and not a development default in production")
		}
		if c.Database.Driver == "postgres" && c.Database.URL == "" {
			return fmt.Errorf("DATABASE_URL is required when DATABASE_DRIVER=postgres")
		}
	}
	return nil
}

func (c *Config) Chains() map[types.BlockchainType]ChainConfig {
	return map[types.BlockchainType]ChainConfig{
		types.BlockchainEthereum: c.Blockchain.Ethereum,
		types.BlockchainBitcoin:  c.Blockchain.Bitcoin,
		types.BlockchainSolana:   c.Blockchain.Solana,
		types.BlockchainCardano:  c.Blockchain.Cardano,
	}
}

func (c *Config) IsProduction() bool {
	return c.Environment == "production"
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return n
}

func envFloat(key string, fallback float64) float64 {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return fallback
	}
	return n
}

func networkFromEnv(key string, fallback types.BlockchainNetwork) types.BlockchainNetwork {
	value := env(key, "")
	if value == "" {
		return fallback
	}
	return types.BlockchainNetwork(strings.ToLower(strings.TrimSpace(value)))
}
