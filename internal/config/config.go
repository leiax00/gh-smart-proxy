// Package config resolves the proxy's runtime configuration, in order of
// decreasing precedence: environment variables, an optional YAML config file
// pointed at by CONFIG_PATH, a link-time default, then built-in defaults.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Secret is the link-time default proxy secret. Bake a value in at build time:
//
//	go build -ldflags="-X gh-smart-proxy/internal/config.Secret=your-secret"
//
// At runtime the PROXY_SECRET environment variable and the config file both
// take precedence over this value.
var Secret string

// DefaultAllowedHosts is the upstream allow-list used when neither the
// ALLOWED_HOSTS env var nor the config file's allowed_hosts is set. It is
// restricted to GitHub hosts so that, by default, the proxy cannot be turned
// into an arbitrary open proxy or SSRF relay.
var DefaultAllowedHosts = []string{
	"github.com",
	"www.github.com",
	"raw.githubusercontent.com",
	"gist.githubusercontent.com",
	"codeload.github.com",
	"objects.githubusercontent.com",
	"release-assets.githubusercontent.com",
	"github-releases.githubusercontent.com",
}

// Config holds the resolved runtime configuration.
type Config struct {
	Secret       string
	Addr         string
	RateLimit    int
	RateWindow   time.Duration
	AllowedHosts []string
}

// fileConfig is the on-disk YAML representation. A zero value means "unset", so
// the next layer of precedence (env / built-in default) fills it in.
type fileConfig struct {
	Secret            string   `yaml:"secret"`
	Addr              string   `yaml:"addr"`
	RateLimit         int      `yaml:"rate_limit"`
	RateWindowSeconds int      `yaml:"rate_window_seconds"`
	AllowedHosts      []string `yaml:"allowed_hosts"`
}

// Load resolves configuration. An empty Secret is allowed and selects
// open-proxy mode; the caller decides whether to warn. Precedence is:
// environment > config file (CONFIG_PATH) > link-time default > built-in.
func Load() (Config, error) {
	f, err := loadFile(os.Getenv("CONFIG_PATH"))
	if err != nil {
		return Config{}, err
	}

	rateLimit := 120
	if n, ok := envInt("RATE_LIMIT"); ok {
		rateLimit = n
	} else if f.RateLimit > 0 {
		rateLimit = f.RateLimit
	}

	rateWindowSec := 60
	if n, ok := envInt("RATE_WINDOW_SECONDS"); ok {
		rateWindowSec = n
	} else if f.RateWindowSeconds > 0 {
		rateWindowSec = f.RateWindowSeconds
	}

	return Config{
		Secret:       firstNonEmpty(os.Getenv("PROXY_SECRET"), f.Secret, Secret),
		Addr:         firstNonEmpty(os.Getenv("ADDR"), f.Addr, ":8080"),
		RateLimit:    rateLimit,
		RateWindow:   time.Duration(rateWindowSec) * time.Second,
		AllowedHosts: resolveAllowedHosts(os.Getenv("ALLOWED_HOSTS"), f.AllowedHosts),
	}, nil
}

// resolveAllowedHosts picks the upstream allow-list. The ALLOWED_HOSTS env var
// (comma-separated) wins, then the config file's allowed_hosts, then the
// built-in DefaultAllowedHosts. An empty selection never collapses to
// "allow everything" — the safe defaults always apply.
func resolveAllowedHosts(envVal string, fileHosts []string) []string {
	var src []string
	switch {
	case envVal != "":
		src = strings.Split(envVal, ",")
	case len(fileHosts) > 0:
		src = fileHosts
	default:
		return DefaultAllowedHosts
	}
	out := make([]string, 0, len(src))
	for _, h := range src {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			out = append(out, h)
		}
	}
	if len(out) == 0 {
		return DefaultAllowedHosts
	}
	return out
}

// loadFile reads and parses the YAML config at path. An empty path yields a
// zero fileConfig (no file loaded). A missing or invalid file is an error.
func loadFile(path string) (fileConfig, error) {
	var f fileConfig
	if path == "" {
		return f, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return f, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("parse config %s: %w", path, err)
	}
	return f, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// envInt parses name as a positive int; ok is false when unset or invalid.
func envInt(name string) (int, bool) {
	s := os.Getenv(name)
	if s == "" {
		return 0, false
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
