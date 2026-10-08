package auth

import (
	"sync"
	"time"
)

// Limiter counts failed logins per IP and globally.
type Limiter struct {
	mu     sync.Mutex
	byIP   map[string][]time.Time
	global []time.Time
	Now    func() time.Time
	Window time.Duration
	PerIP  int
	Global int
}

func (l *Limiter) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// Blocked reports whether ip is currently over the limit.
func (l *Limiter) Blocked(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.blocked(ip)
}

// Fail records a failed attempt and reports whether the limit is now exceeded.
func (l *Limiter) Fail(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.byIP = pruneMap(l.byIP, now, l.window())
	l.global = prune(l.global, now, l.window())
	if l.byIP == nil {
		l.byIP = map[string][]time.Time{}
	}
	l.byIP[ip] = append(l.byIP[ip], now)
	l.global = append(l.global, now)
	return l.blocked(ip)
}

// Reset clears the per-IP counter after a successful login.
func (l *Limiter) Reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byIP, ip)
}

func (l *Limiter) blocked(ip string) (bool, time.Duration) {
	now := l.now()
	window := l.window()
	ipHits := prune(l.byIP[ip], now, window)
	global := prune(l.global, now, window)
	if l.byIP != nil {
		l.byIP[ip] = ipHits
	}
	l.global = global
	perIP := l.PerIP
	if perIP <= 0 {
		perIP = 5
	}
	glob := l.Global
	if glob <= 0 {
		glob = 20
	}
	if len(ipHits) >= perIP {
		return true, retryAfter(ipHits, now, window)
	}
	if len(global) >= glob {
		return true, retryAfter(global, now, window)
	}
	return false, 0
}

func (l *Limiter) window() time.Duration {
	if l.Window <= 0 {
		return 15 * time.Minute
	}
	return l.Window
}

func pruneMap(in map[string][]time.Time, now time.Time, window time.Duration) map[string][]time.Time {
	if in == nil {
		return map[string][]time.Time{}
	}
	for key, hits := range in {
		kept := prune(hits, now, window)
		if len(kept) == 0 {
			delete(in, key)
			continue
		}
		in[key] = kept
	}
	return in
}

func prune(hits []time.Time, now time.Time, window time.Duration) []time.Time {
	out := hits[:0]
	for _, hit := range hits {
		if now.Sub(hit) < window {
			out = append(out, hit)
		}
	}
	return out
}

func retryAfter(hits []time.Time, now time.Time, window time.Duration) time.Duration {
	if len(hits) == 0 {
		return window
	}
	oldest := hits[0]
	for _, hit := range hits[1:] {
		if hit.Before(oldest) {
			oldest = hit
		}
	}
	wait := window - now.Sub(oldest)
	if wait < time.Second {
		return time.Second
	}
	return wait
}
