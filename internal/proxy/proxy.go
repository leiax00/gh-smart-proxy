// Package proxy implements the authenticated GitHub reverse proxy.
//
// Requests to /<secret>/<https-url> are authenticated against the secret,
// validated against the GitHub host allow-list, and forwarded to GitHub with
// hop-by-hop and credential headers stripped.
package proxy

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	revproxy "net/http/httputil"
	"net/url"
	"strings"
	"time"

	"gh-smart-proxy/internal/httputil"
)

// allowedHosts are the upstream hosts the proxy is willing to forward to.
var allowedHosts = map[string]bool{
	"github.com":                            true,
	"www.github.com":                        true,
	"raw.githubusercontent.com":             true,
	"gist.githubusercontent.com":            true,
	"codeload.github.com":                   true,
	"objects.githubusercontent.com":         true,
	"release-assets.githubusercontent.com":  true,
	"github-releases.githubusercontent.com": true,
}

// New returns an http.Handler that authenticates the request against secret,
// parses the upstream target from the URL, and reverse-proxies it to GitHub.
func New(secret string) http.Handler {
	rp := &revproxy.ReverseProxy{
		Director: func(r *http.Request) {},
		ModifyResponse: func(resp *http.Response) error {
			applyCacheHeaders(resp)
			httputil.StripHopHeaders(resp.Header)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy error: %s %s: %v", r.Method, r.URL.String(), err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   20 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, err := parseTarget(r, secret)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		log.Printf("%s %s -> %s", httputil.ClientIP(r), r.Method, target.String())

		r.URL = target
		r.Host = target.Host
		r.RequestURI = ""

		// Never forward client credentials to GitHub.
		r.Header.Del("Authorization")
		r.Header.Del("Proxy-Authorization")
		r.Header.Del("Cookie")
		httputil.StripHopHeaders(r.Header)

		r.Header.Set("Host", target.Host)
		r.Header.Set("X-Forwarded-Host", r.Host)
		r.Header.Set("X-Forwarded-Proto", httputil.Scheme(r))

		rp.ServeHTTP(w, r)
	})
}

// parseTarget validates the request against the secret prefix and parses the
// upstream https:// URL, enforcing the host allow-list.
func parseTarget(r *http.Request, secret string) (*url.URL, error) {
	prefix := "/" + secret + "/"
	if !strings.HasPrefix(r.URL.EscapedPath(), prefix) && !strings.HasPrefix(r.URL.Path, prefix) {
		return nil, errors.New("forbidden")
	}

	raw := strings.TrimPrefix(r.URL.Path, prefix)
	if raw == "" {
		return nil, errors.New("empty target")
	}

	if !strings.HasPrefix(raw, "https://") {
		return nil, errors.New("target must start with https://")
	}

	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "https" || target.Host == "" {
		return nil, errors.New("invalid target")
	}
	target.RawQuery = r.URL.RawQuery

	host := strings.ToLower(target.Hostname())
	if !allowedHosts[host] {
		return nil, fmt.Errorf("host not allowed: %s", host)
	}
	return target, nil
}

// applyCacheHeaders sets a CDN-friendly cache policy: cacheable GitHub assets
// get long-lived public caching; everything else (including git smart HTTP) is
// no-store.
func applyCacheHeaders(resp *http.Response) {
	path := resp.Request.URL.Path
	host := strings.ToLower(resp.Request.URL.Hostname())

	// Git smart HTTP must not be cached.
	if strings.Contains(path, "/info/refs") || strings.Contains(path, "git-upload-pack") || strings.Contains(path, "git-receive-pack") || resp.Request.Method != http.MethodGet {
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Set("CDN-Cache-Control", "no-store")
		return
	}

	cacheable := host == "raw.githubusercontent.com" ||
		host == "codeload.github.com" ||
		host == "objects.githubusercontent.com" ||
		host == "release-assets.githubusercontent.com" ||
		host == "github-releases.githubusercontent.com" ||
		(host == "github.com" && strings.Contains(path, "/releases/download/")) ||
		(host == "github.com" && strings.Contains(path, "/archive/refs/"))

	if cacheable && resp.StatusCode >= 200 && resp.StatusCode < 400 {
		resp.Header.Set("Cache-Control", "public, max-age=86400, s-maxage=604800")
		resp.Header.Set("CDN-Cache-Control", "public, max-age=604800")
	} else {
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Set("CDN-Cache-Control", "no-store")
	}
}
