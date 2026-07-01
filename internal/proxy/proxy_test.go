package proxy

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

func TestParseTargetReleaseDownloadURL(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://gh.example.com/example-user/https://github.com/example-user/ax-cli/releases/download/v0.1.0/ax-linux-x86_64", nil)
	if err != nil {
		t.Fatal(err)
	}

	target, err := parseTarget(req, "example-user", map[string]bool{"github.com": true})
	if err != nil {
		t.Fatalf("parseTarget returned error: %v", err)
	}

	want := "https://github.com/example-user/ax-cli/releases/download/v0.1.0/ax-linux-x86_64"
	if got := target.String(); got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

func TestRewriteRedirectLocationKeepsReleaseAssetBehindProxy(t *testing.T) {
	reqURL, err := url.Parse("https://github.com/example-user/ax-cli/releases/download/v0.1.0/ax-linux-x86_64")
	if err != nil {
		t.Fatal(err)
	}
	req := &http.Request{URL: reqURL}
	req = req.WithContext(context.WithValue(req.Context(), proxyBaseKey, "https://gh.example.com"))

	resp := &http.Response{
		StatusCode: http.StatusFound,
		Header:     make(http.Header),
		Request:    req,
	}
	resp.Header.Set("Location", "https://release-assets.githubusercontent.com/github-production-release-asset/1215770056/file?sig=abc")

	rewriteRedirectLocation(resp, "example-user", map[string]bool{"release-assets.githubusercontent.com": true})

	want := "https://gh.example.com/example-user/https://release-assets.githubusercontent.com/github-production-release-asset/1215770056/file?sig=abc"
	if got := resp.Header.Get("Location"); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestRewriteRedirectLocationIgnoresDisallowedHost(t *testing.T) {
	reqURL, err := url.Parse("https://github.com/owner/repo/releases/download/v1/file")
	if err != nil {
		t.Fatal(err)
	}
	req := &http.Request{URL: reqURL}
	req = req.WithContext(context.WithValue(req.Context(), proxyBaseKey, "https://gh.example.com"))

	resp := &http.Response{
		StatusCode: http.StatusFound,
		Header:     make(http.Header),
		Request:    req,
	}
	resp.Header.Set("Location", "https://example.com/file")

	rewriteRedirectLocation(resp, "example-user", map[string]bool{"github.com": true})

	if got := resp.Header.Get("Location"); got != "https://example.com/file" {
		t.Fatalf("Location = %q, want original disallowed URL", got)
	}
}
