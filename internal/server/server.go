// Package server wires the proxy's handlers and middleware into a single
// configured *http.Server.
package server

import (
	"io"
	"net/http"
	"time"

	"gh-smart-proxy/internal/config"
	"gh-smart-proxy/internal/httputil"
	"gh-smart-proxy/internal/proxy"
	"gh-smart-proxy/internal/ratelimit"
	"gh-smart-proxy/internal/web"
)

// New builds the HTTP server from the resolved configuration.
func New(cfg config.Config) *http.Server {
	lim := ratelimit.New(cfg.RateLimit, cfg.RateWindow)
	proxyHandler := proxy.New(cfg.Secret, cfg.AllowedHosts)
	home := web.Handler(cfg.AllowedHosts)

	// Routing is an explicit switch rather than ServeMux patterns because the
	// proxy path is /<secret>/<https-url> and must be matched by prefix.
	h := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			_, _ = io.WriteString(w, "ok\n")
			return
		case r.URL.Path == "/favicon.ico":
			w.WriteHeader(http.StatusNoContent)
			return
		case r.URL.Path == "/" && r.Method == http.MethodGet:
			home.ServeHTTP(w, r)
			return
		}

		if !lim.Allow(httputil.ClientIP(r)) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		proxyHandler.ServeHTTP(w, r)
	}

	// Read/Write timeouts are left at zero on purpose: git clone over smart HTTP
	// streams for a long time and must not be cut off.
	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           http.HandlerFunc(h),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}
