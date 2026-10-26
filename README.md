# go-valkey

A token bucket rate limiter for Go that controls how many operations per second are allowed, with burst support.

```go
package main

import (
	"fmt"
	"time"

	go_valkey "go_valkey"
)

func main() {
	lim := go_valkey.New(10, 100, func() time.Duration {
		return time.Duration(time.Now().UnixNano())
	})
	if lim.Allow() {
		fmt.Println("request allowed")
	}
	if lim.AllowN(5) {
		fmt.Println("batch allowed")
	}
	fmt.Println("tokens remaining:", lim.Tokens())
}
```

## Why this exists

Admission control at the call site — a goroutine making outbound requests, a worker draining a queue — needs to answer one question fast: *may I proceed right now?* This limiter answers that without blocking, without external state, and without third-party dependencies. The trade-off is that it is in-process only; if you need to rate-limit across multiple processes or machines you need a shared store, and this library will not help with that.

## API

- `New(refillRate float64, capacity float64, clock func() time.Duration) *Limiter` — construct a limiter. `refillRate` is tokens per second, `capacity` is the burst size (the bucket starts full). `clock` returns the current time as a duration since some fixed epoch; pass `nil` to use the system clock.
- `Allow() bool` — consume one token; returns `true` if granted.
- `AllowN(n float64) bool` — consume `n` tokens; returns `false` if `n` exceeds `capacity` (such a request could never succeed).
- `Tokens() float64` — current token count, for observability only. Do not use this to make admission decisions; the value is stale the instant you read it.

## Edge cases worth knowing

The clock is injected as a function returning a `time.Duration`. This is deliberate: it makes tests deterministic by letting the caller advance time explicitly, and it lets you swap in any monotonic source. If your clock goes backwards (non-monotonic source, reset, manual override) the limiter clamps elapsed time to zero rather than crediting tokens — a backwards clock should never give you more capacity.

`AllowN` with `n` greater than `capacity` always returns `false`. It does not panic, because the request is valid in shape; it just can never be satisfied. `AllowN` with `n <= 0` panics, because consuming zero or negative tokens is a programming error.

`New` panics if `refillRate` or `capacity` is not positive. A limiter that can never refill or never hold tokens is meaningless, and failing loudly at construction is better than silently misbehaving later.
