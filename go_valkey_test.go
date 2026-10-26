package go_valkey

import (
	"math"
	"testing"
	"time"
)

// fakeClock is a controllable time source. It holds a duration that tests
// advance explicitly, so no test ever depends on wall-clock time or sleeps.
type fakeClock struct {
	t time.Duration
}

func (f *fakeClock) now() time.Duration { return f.t }

// advance moves the clock forward by d.
func (f *fakeClock) advance(d time.Duration) { f.t += d }

func TestNewPanicsOnNonPositiveRate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for non-positive refillRate")
	}
	}()
	New(0, 10, nil)
}

func TestNewPanicsOnNonPositiveCapacity(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for non-positive capacity")
	}
	}()
	New(10, 0, nil)
}

func TestBurstAtStart(t *testing.T) {
	clock := &fakeClock{}
	lim := New(1, 5, clock.now)

	// Bucket starts full, so five requests succeed and the sixth fails.
	for i := 0; i < 5; i++ {
		if !lim.Allow() {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if lim.Allow() {
		t.Fatal("sixth request should be denied")
	}
}

func TestRefillOverTime(t *testing.T) {
	clock := &fakeClock{}
	lim := New(2, 4, clock.now) // 2 tokens/sec, capacity 4

	// Drain the bucket.
	for i := 0; i < 4; i++ {
		lim.Allow()
	}
	if lim.Allow() {
		t.Fatal("bucket should be empty")
	}

	// Advance 500ms: at 2/sec that is exactly 1 token.
	clock.advance(500 * time.Millisecond)
	if !lim.Allow() {
		t.Fatal("one token should be available after 500ms")
	}
	if lim.Allow() {
		t.Fatal("only one token should have been refilled")
	}
}

func TestRefillCappedAtCapacity(t *testing.T) {
	clock := &fakeClock{}
	lim := New(10, 3, clock.now)

	// Drain.
	for i := 0; i < 3; i++ {
		lim.Allow()
	}

	// Advance far beyond what would refill to capacity.
	clock.advance(10 * time.Second)
	// Capacity is 3, so only 3 tokens available regardless of elapsed time.
	for i := 0; i < 3; i++ {
		if !lim.Allow() {
			t.Fatalf("request %d should succeed within capacity", i)
		}
	}
	if lim.Allow() {
		t.Fatal("should be denied after exhausting capacity")
	}
}

func TestAllowNGreaterThanCapacityAlwaysDenied(t *testing.T) {
	clock := &fakeClock{}
	lim := New(1, 5, clock.now)
	if lim.AllowN(6) {
		t.Fatal("AllowN exceeding capacity should always return false")
	}
}

func TestAllowNPanicOnNonPositive(t *testing.T) {
	clock := &fakeClock{}
	lim := New(1, 5, clock.now)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for non-positive n")
		}
	}()
	lim.AllowN(0)
}

func TestAllowNConsumesMultiple(t *testing.T) {
	clock := &fakeClock{}
	lim := New(1, 5, clock.now)

	// Consume 3 at once.
	if !lim.AllowN(3) {
		t.Fatal("AllowN(3) should succeed")
	}
	// 2 remain.
	if !lim.Allow() {
		t.Fatal("Allow should succeed, 2 tokens left")
	}
	if !lim.Allow() {
		t.Fatal("Allow should succeed, 1 token left")
	}
	if lim.Allow() {
		t.Fatal("Allow should fail, bucket empty")
	}
}

func TestClockMovingBackwards(t *testing.T) {
	clock := &fakeClock{}
	lim := New(1, 3, clock.now)

	// Consume all.
	for i := 0; i < 3; i++ {
		lim.Allow()
	}

	// Move the clock backwards. The limiter must not credit tokens for
	// negative elapsed time; it should clamp to zero and remain empty.
	clock.advance(-1 * time.Second)
	if lim.Allow() {
		t.Fatal("should be denied when clock moves backwards")
	}
}

func TestTokensForObservability(t *testing.T) {
	clock := &fakeClock{}
	lim := New(1, 5, clock.now)

	if math.Abs(lim.Tokens()-5.0) > 1e-9 {
		t.Fatalf("expected 5 tokens at start, got %f", lim.Tokens())
	}

	lim.AllowN(2)
	if math.Abs(lim.Tokens()-3.0) > 1e-9 {
		t.Fatalf("expected 3 tokens after consuming 2, got %f", lim.Tokens())
	}
}

func TestConcurrentAccessDoesNotDeadlock(t *testing.T) {
	clock := &fakeClock{}
	lim := New(100, 1000, clock.now)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				lim.Allow()
			}
			close(done)
		}()
	}

	// The goroutines run without advancing the clock. If the mutex is
	// correct they all finish quickly; if Allow deadlocks the test
	// timeout catches it.
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestFractionalRefillAccumulates(t *testing.T) {
	clock := &fakeClock{}
	lim := New(2, 4, clock.now) // 2 tokens/sec

	// Drain.
	for i := 0; i < 4; i++ {
		lim.Allow()
	}

	// 250ms gives 0.5 token — not enough yet.
	clock.advance(250 * time.Millisecond)
	if lim.Allow() {
		t.Fatal("0.5 token should not allow a request")
	}

	// Another 250ms brings it to 1.0.
	clock.advance(250 * time.Millisecond)
	if !lim.Allow() {
		t.Fatal("1.0 token should allow a request")
	}
}

func TestDefaultClockIsNotNil(t *testing.T) {
	// Passing nil for the clock should use a real wall-clock source.
	// We cannot assert exact behaviour, only that construction succeeds
	// and a call works without panicking.
	lim := New(1, 1, nil)
	_ = lim.Allow()
}
