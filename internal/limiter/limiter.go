package limiter

import (
	"sync/atomic"
)

// RateLimiter tracks global request and connection rates.
// It is safe for concurrent use.
type RateLimiter struct {
	reqCount  int64
	connCount int64
}

// New creates a new RateLimiter instance.
func New() *RateLimiter {
	return &RateLimiter{}
}

// Reset resets the request and connection counts to zero.
// This should be called periodically (e.g., every second).
func (rl *RateLimiter) Reset() {
	atomic.StoreInt64(&rl.reqCount, 0)
	atomic.StoreInt64(&rl.connCount, 0)
}

// IncReq increments the request counter.
func (rl *RateLimiter) IncReq() {
	atomic.AddInt64(&rl.reqCount, 1)
}

// IncConn increments the connection counter.
func (rl *RateLimiter) IncConn() {
	atomic.AddInt64(&rl.connCount, 1)
}

// GetCounts returns the current request and connection counts.
func (rl *RateLimiter) GetCounts() (int64, int64) {
	return atomic.LoadInt64(&rl.reqCount), atomic.LoadInt64(&rl.connCount)
}
