// Package httputil holds small helpers for inspecting and mutating HTTP
// requests, shared across the proxy's packages.
package httputil

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP extracts the originating client IP, honoring the CF-Connecting-IP
// and X-Forwarded-For headers set by upstream proxies.
func ClientIP(r *http.Request) string {
	if cf := r.Header.Get("CF-Connecting-IP"); cf != "" {
		return cf
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Scheme returns the request scheme, preferring the X-Forwarded-Proto header.
func Scheme(r *http.Request) string {
	if s := r.Header.Get("X-Forwarded-Proto"); s != "" {
		return s
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// PublicBaseURL reconstructs the public origin (scheme://host) for the request.
func PublicBaseURL(r *http.Request) string {
	host := r.Host
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		host = xfh
	}
	return Scheme(r) + "://" + host
}

// StripHopHeaders removes hop-by-hop headers that must not traverse a proxy.
func StripHopHeaders(h http.Header) {
	for _, k := range []string{"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(k)
	}
}
