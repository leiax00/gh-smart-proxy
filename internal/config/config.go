// Package config resolves the proxy's runtime configuration from the
// environment, with a link-time default fallback for the secret.
package config

import (
	"fmt"
	"os"
	"time"
)

// Secret is the link-time default proxy secret. Bake a value in at build time:
//
//	go build -ldflags="-X gh-smart-proxy/internal/config.Secret=your-secret"
//
// At runtime the PROXY_SECRET environment variable takes precedence.
var Secret string

// Config holds the resolved runtime configuration.
type Config struct {
	Secret     string
	Addr       string
	RateLimit  int
	RateWindow time.Duration
}

// Load reads configuration from the environment, applying defaults. It does not
// validate; callers are responsible for rejecting an empty Secret.
func Load() Config {
	secret := os.Getenv("PROXY_SECRET")
	if secret == "" {
		secret = Secret
	}
	return Config{
		Secret:     secret,
		Addr:       env("ADDR", ":8080"),
		RateLimit:  atoi("RATE_LIMIT", 120),
		RateWindow: time.Duration(atoi("RATE_WINDOW_SECONDS", 60)) * time.Second,
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoi(envVar string, def int) int {
	var n int
	if _, err := fmt.Sscanf(os.Getenv(envVar), "%d", &n); err != nil || n <= 0 {
		return def
	}
	return n
}
