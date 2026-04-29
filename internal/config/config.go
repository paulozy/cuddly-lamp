package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	API      APIConfig
	OAuth    OAuthConfig
	Log      LogConfig
}

type ServerConfig struct {
	Port            string
	Env             string
	JWTSecret       string
	JWTIssuer       string
	JWTAudience     string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	EncryptionKey   string
}

type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
	DSN      string
}

type RedisConfig struct {
	Host     string
	Port     int
	DB       int
	Password string
}

type APIConfig struct {
	AnthropicAPIKey         string
	AnthropicTokensPerHour  int
	GithubToken             string
	WebhookBaseURL          string // public base URL for webhook endpoints, e.g. https://api.example.com
	GitHubPRReviewEnabled   bool   // whether to post PR reviews
}

type LogConfig struct {
	Level string
}

type OAuthProviderConfig struct {
	ClientID     string
	ClientSecret string
	CallbackURL  string
}

type OAuthConfig struct {
	Providers map[string]OAuthProviderConfig
}

func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port:            getEnv("PORT", "3000"),
			Env:             getEnv("ENV", "development"),
			JWTSecret:       getEnv("JWT_SECRET", "supersecretkey"),
			JWTIssuer:       getEnv("JWT_ISSUER", "idp-backend"),
			JWTAudience:     getEnv("JWT_AUDIENCE", "idp-users"),
			AccessTokenTTL:  time.Duration(getEnvInt("ACCESS_TOKEN_TTL", 15)) * time.Minute,     // in minutes
			RefreshTokenTTL: time.Duration(getEnvInt("REFRESH_TOKEN_TTL", 10080)) * time.Minute, // in minutes (7 days)
			EncryptionKey:   getEnv("ENCRYPTION_KEY", ""),
		},
		Database: newDatabaseConfig(),
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnvInt("REDIS_PORT", 6379),
			DB:       getEnvInt("REDIS_DB", 0),
			Password: getEnv("REDIS_PASSWORD", ""),
		},
		API: APIConfig{
			AnthropicAPIKey:        getEnv("ANTHROPIC_API_KEY", ""),
			AnthropicTokensPerHour: getEnvInt("ANTHROPIC_TOKENS_PER_HOUR", 20000),
			GithubToken:            getEnv("GITHUB_TOKEN", ""),
			WebhookBaseURL:         getEnv("WEBHOOK_BASE_URL", ""),
			GitHubPRReviewEnabled:  getEnvBool("GITHUB_PR_REVIEW_ENABLED", false),
		},
		OAuth: OAuthConfig{
			Providers: map[string]OAuthProviderConfig{
				"github": {
					ClientID:     getEnv("GITHUB_CLIENT_ID", ""),
					ClientSecret: getEnv("GITHUB_CLIENT_SECRET", ""),
					CallbackURL:  getEnv("GITHUB_CALLBACK_URL", ""),
				},
				"gitlab": {
					ClientID:     getEnv("GITLAB_CLIENT_ID", ""),
					ClientSecret: getEnv("GITLAB_CLIENT_SECRET", ""),
					CallbackURL:  getEnv("GITLAB_CALLBACK_URL", ""),
				},
			},
		},
		Log: LogConfig{
			Level: getEnv("LOG_LEVEL", "info"),
		},
	}
}

func newDatabaseConfig() DatabaseConfig {
	host := getEnv("DB_HOST", "localhost")
	port := getEnvInt("DB_PORT", 5432)
	user := getEnv("DB_USER", "postgres")
	password := getEnv("DB_PASSWORD", "postgres")
	dbName := getEnv("DB_NAME", "idp_dev")
	sslMode := getEnv("DB_SSL_MODE", "disable")

	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbName, sslMode,
	)

	return DatabaseConfig{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		Name:     dbName,
		SSLMode:  sslMode,
		DSN:      dsn,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}
