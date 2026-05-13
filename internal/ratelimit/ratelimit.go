// Package ratelimit provides a simple sliding-window rate limiter.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter is a per-tool sliding-window rate limiter.
// Limiter is safe for concurrent use.
type Limiter struct {
	maxPerMinute int
	mu           sync.Mutex
	requests     []time.Time
}

// New creates a Limiter that allows at most maxPerMinute requests per 60-second
// sliding window. If maxPerMinute is 0, all requests are allowed.
func New(maxPerMinute int) *Limiter {
	return &Limiter{maxPerMinute: maxPerMinute}
}

// Allow reports whether the request is within the rate limit.
// It records the request timestamp when returning true.
func (l *Limiter) Allow() bool {
	if l.maxPerMinute == 0 {
		return true
	}

	now := time.Now()
	window := now.Add(-time.Minute)

	l.mu.Lock()
	defer l.mu.Unlock()

	i := 0
	for i < len(l.requests) && l.requests[i].Before(window) {
		i++
	}

	l.requests = l.requests[i:]

	if len(l.requests) >= l.maxPerMinute {
		return false
	}

	l.requests = append(l.requests, now)
	return true
}
