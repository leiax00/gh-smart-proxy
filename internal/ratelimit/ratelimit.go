// Package ratelimit provides a simple per-IP, fixed-window rate limiter.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter caps the number of allowed requests per IP within a rolling window.
type Limiter struct {
	mu     sync.Mutex
	states map[string]*state
	limit  int
	window time.Duration
}

type state struct {
	count int
	reset time.Time
}

// New returns a Limiter that allows at most limit requests per window per IP.
func New(limit int, window time.Duration) *Limiter {
	return &Limiter{states: make(map[string]*state), limit: limit, window: window}
}

// Allow reports whether ip may make another request within the current window.
func (l *Limiter) Allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	st, ok := l.states[ip]
	if !ok || now.After(st.reset) {
		l.states[ip] = &state{count: 1, reset: now.Add(l.window)}
		return true
	}
	if st.count >= l.limit {
		return false
	}
	st.count++
	return true
}
