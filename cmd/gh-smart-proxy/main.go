// Command gh-smart-proxy is a lightweight HTTPS reverse proxy for GitHub with
// URL-secret authentication, a host allow-list, per-IP rate limiting, and a
// built-in landing page.
package main

import (
	"log"

	"gh-smart-proxy/internal/config"
	"gh-smart-proxy/internal/server"
)

func main() {
	cfg := config.Load()
	if cfg.Secret == "" {
		log.Fatal("secret is required")
	}

	srv := server.New(cfg)
	log.Printf("gh-smart-proxy listening on %s", cfg.Addr)
	log.Fatal(srv.ListenAndServe())
}
