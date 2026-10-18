package logic

import (
	"sync"
	"time"
)

type limitBucket struct {
	count   int
	expires time.Time
}
type LimiterMetrics struct {
	Keys                           int
	RateRejected, CapacityRejected uint64
}

// shortcut: single-process limits reset on restart; use shared storage before adding replicas.
type Limiter struct {
	mu                             sync.Mutex
	buckets                        map[string]limitBucket
	maxKeys                        int
	rateRejected, capacityRejected uint64
}

func NewLimiter(maxKeys int) *Limiter {
	return &Limiter{buckets: make(map[string]limitBucket), maxKeys: maxKeys}
}
func (l *Limiter) Allow(key string, limit int, window time.Duration, now time.Time) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if !now.Before(b.expires) {
			delete(l.buckets, k)
		}
	}
	b, exists := l.buckets[key]
	if !exists {
		if len(l.buckets) >= l.maxKeys || limit <= 0 || window <= 0 {
			l.capacityRejected++
			return false, 1
		}
		b = limitBucket{expires: now.Add(window)}
	}
	if b.count >= limit {
		l.rateRejected++
		return false, max(1, int((b.expires.Sub(now)+time.Second-1)/time.Second))
	}
	b.count++
	l.buckets[key] = b
	return true, 0
}
func (l *Limiter) Metrics() LimiterMetrics {
	l.mu.Lock()
	defer l.mu.Unlock()
	return LimiterMetrics{len(l.buckets), l.rateRejected, l.capacityRejected}
}
