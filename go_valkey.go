package go_valkey

import (
	"sync"
	"time"
)

// Limiter is a token bucket rate limiter.
//
// The bucket has a fixed capacity (burst size) and refills at a constant
// rate (refillRate tokens per second). Each call to Allow checks whether
// at least one token is available; if so, one token is consumed and Allow
// returns true. Otherwise it returns false immediately without blocking.
//
// A clock function is injected so behaviour is deterministic in tests.
// This avoids any dependency on wall-clock time, which would make tests
// flaky under CI load. Returning a duration from the clock makes time
// advancement explicit and controllable by the caller.
type Limiter struct {
	mu              sync.Mutex
	refillRate      float64       // tokens added per second
capacity         float64       // maximum tokens the bucket can hold
	tokens          float64       // current token count
	lastRefill      time.Duration // time of last refill, from the clock
	now             func() time.Duration
}

// New creates a Limiter with the given refill rate (tokens per second) and
// capacity (burst size). The clock function returns the current time as a
// duration since some fixed epoch; the limiter never inspects the epoch,
// only the difference between successive calls, so any monotonic source
// works. The bucket starts full so the first burst of requests succeeds.
//
// Panics if refillRate or capacity is not positive, since a zero or
// negative rate would make the limiter meaningless and a zero capacity
// would reject every request.
func New(refillRate float64, capacity float64, clock func() time.Duration) *Limiter {
	if refillRate <= 0 {
		panic("go_valkey: refillRate must be positive")
	}
	if capacity <= 0 {
		panic("go_valkey: capacity must be positive")
	}
	if clock == nil {
		clock = func() time.Duration { return time.Duration(time.Now().UnixNano()) }
	}
	start := clock()
	return &Limiter{
		refillRate: refillRate,
		capacity:   capacity,
		// Start full so the caller can immediately use the full burst.
		// Starting empty would silently reject the very first requests,
		// which is rarely what the caller expects.
		tokens:     capacity,
		lastRefill: start,
		now:        clock,
	}
}

// Allow reports whether one token is available, consuming it if so.
// It never blocks. Concurrent calls are serialised by a mutex, so the
// limiter is safe to share across goroutines.
func (l *Limiter) Allow() bool {
	return l.AllowN(1)
}

// AllowN reports whether n tokens are available, consuming them if so.
// If n exceeds the bucket capacity the request always returns false,
// since such a request could never succeed even after waiting.
// n must be positive; a non-positive value panics because consuming
// zero or negative tokens is a programming error, not a rate-limit
// decision.
func (l *Limiter) AllowN(n float64) bool {
	if n <= 0 {
		panic("go_valkey: n must be positive")
	}
	if n > l.capacity {
		return false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	elapsed := l.now() - l.lastRefill
	if elapsed < 0 {
		// The clock went backwards. This can happen if the source is
		// non-monotonic or reset. Refusing to advance state keeps the
		// bucket consistent rather than inflating tokens.
		elapsed = 0
	}
	l.lastRefill += elapsed

	// Refill proportionally to elapsed time, capped at capacity.
	l.tokens += float64(elapsed) / float64(time.Second) * l.refillRate
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}

	if l.tokens >= n {
		l.tokens -= n
		return true
	}
	return false
}

// Tokens returns the current number of available tokens. This value may
// already be stale by the time the caller inspects it, so it is intended
// only for observability — dashboards, logging, debugging — not for
// making admission decisions. Decisions must go through Allow or AllowN.
func (l *Limiter) Tokens() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.tokens
}
