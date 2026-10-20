package config

import "os"

type Config struct {
	DatabaseURL string
	Port        string
	JWTSecret   string
}

// Load reads configuration from the real environment. Every key is read
// directly via os.Getenv so values that are genuinely set in the
// environment (e.g. DATABASE_URL pointing at the per-migration Postgres
// sidecar) are always returned, never silently defaulted to "".
func Load() (*Config, error) {
	return &Config{
		Port:        os.Getenv("PORT"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
	}, nil
}
