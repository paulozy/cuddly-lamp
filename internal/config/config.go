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
	Host string
	Port int
	DB   int
}

type APIConfig struct {
	AnthropicAPIKey string
	GithubToken     string
}

type LogConfig struct {
	Level string
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
		},
		Database: newDatabaseConfig(),
		Redis: RedisConfig{
			Host: getEnv("REDIS_HOST", "localhost"),
			Port: getEnvInt("REDIS_PORT", 6379),
			DB:   getEnvInt("REDIS_DB", 0),
		},
		API: APIConfig{
			AnthropicAPIKey: getEnv("ANTHROPIC_API_KEY", ""),
			GithubToken:     getEnv("GITHUB_TOKEN", ""),
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
