package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Config holds the runtime configuration for the Wompi Webhook Gateway.
// Every setting is driven by environment variables with zero hardcoded credentials.
type Config struct {
	Port               string
	DatabaseURL        string
	AdminAPIKey        string
	AutoMigrate        bool
	WorkerConcurrency  int
	WorkerPollInterval time.Duration
	ForwardTimeout     time.Duration
	GinMode            string
}

// Load reads environment variables and returns a validated Config.
func Load() (*Config, error) {
	dbURL := getEnv("DATABASE_URL", "")
	if dbURL == "" {
		dbURL = buildDatabaseURLFromParts()
	}

	cfg := &Config{
		Port:               getEnv("PORT", "8080"),
		DatabaseURL:        dbURL,
		AdminAPIKey:        getEnv("ADMIN_API_KEY", ""),
		AutoMigrate:        getEnvBool("AUTO_MIGRATE", true),
		WorkerConcurrency:  getEnvInt("WORKER_CONCURRENCY", 8),
		WorkerPollInterval: getEnvDuration("WORKER_POLL_INTERVAL", 5*time.Second),
		ForwardTimeout:     getEnvDuration("HTTP_FORWARD_TIMEOUT", 15*time.Second),
		GinMode:            getEnv("GIN_MODE", "release"),
	}

	if cfg.AdminAPIKey == "" {
		return nil, fmt.Errorf("ADMIN_API_KEY environment variable is required")
	}

	if cfg.WorkerConcurrency < 1 {
		cfg.WorkerConcurrency = 1
	}

	return cfg, nil
}

// buildDatabaseURLFromParts constructs a safe PostgreSQL DSN from individual POSTGRES_* variables
// when DATABASE_URL is not explicitly provided. Using url.URL properly escapes special characters in passwords.
func buildDatabaseURLFromParts() string {
	host := getEnv("POSTGRES_HOST", "")
	user := getEnv("POSTGRES_USER", "")
	pass := getEnv("POSTGRES_PASSWORD", "")
	db := getEnv("POSTGRES_DB", "")
	if host == "" || user == "" || db == "" {
		return ""
	}
	port := getEnv("POSTGRES_PORT", "5432")
	sslMode := getEnv("POSTGRES_SSLMODE", "disable")

	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, pass),
		Host:   fmt.Sprintf("%s:%s", host, port),
		Path:   db,
	}
	q := u.Query()
	q.Set("sslmode", sslMode)
	u.RawQuery = q.Encode()
	return u.String()
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvInt(key string, fallback int) int {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(val)
	if err != nil {
		return fallback
	}
	return parsed
}
