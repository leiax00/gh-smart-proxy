// Command gh-smart-proxy is a lightweight HTTPS reverse proxy for GitHub with
// optional URL-secret authentication, a host allow-list, per-IP rate limiting,
// and a built-in landing page.
package main

import (
	"log"

	"gh-smart-proxy/internal/config"
	"gh-smart-proxy/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.Secret == "" {
		log.Printf("WARNING: PROXY_SECRET is empty — running in open-proxy mode (no URL auth). Only safe behind a trusted frontend or on a private network.")
	}

	srv := server.New(cfg)
	log.Printf("gh-smart-proxy listening on %s", cfg.Addr)
	log.Fatal(srv.ListenAndServe())
}
