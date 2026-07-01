// Package config holds the proxy's runtime configuration.
//
// The proxy secret is resolved in order of precedence:
//  1. the PROXY_SECRET environment variable,
//  2. the link-time default baked into Secret below,
//  3. an empty value, which the program treats as a fatal misconfiguration.
//
// Bake a default into the binary at build time with:
//
//	go build -ldflags="-X gh-smart-proxy/config.Secret=your-secret"
package config

// Secret is the link-time default proxy secret. It is empty unless injected via
// -ldflags at build time. The PROXY_SECRET environment variable takes
// precedence over this value at runtime.
var Secret string

// Config is the resolved runtime configuration handed to the rest of the
// program after main() finishes wiring it up from the environment.
type Config struct {
	Secret string
}
