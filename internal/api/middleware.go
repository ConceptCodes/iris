package api

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type RateLimiter struct {
	mu              sync.Mutex
	visitors        map[string]*visitor
	rate            rate.Limit
	burst           int
	entryTTL        time.Duration
	cleanupInterval time.Duration
	lastCleanup     time.Time
}

func NewRateLimiter(r, b int) *RateLimiter {
	return &RateLimiter{
		visitors:        make(map[string]*visitor),
		rate:            rate.Limit(r),
		burst:           b,
		entryTTL:        3 * time.Minute,
		cleanupInterval: time.Minute,
		lastCleanup:     time.Now(),
	}
}

func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ip, _, err := net.SplitHostPort(req.RemoteAddr)
			if err != nil {
				ip = req.RemoteAddr
			}

			limiter := rl.getVisitor(ip, time.Now())
			if !limiter.Allow() {
				w.Header().Set("Retry-After", "1")
				http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, req)
		})
	}
}

func (rl *RateLimiter) getVisitor(ip string, now time.Time) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if now.Sub(rl.lastCleanup) >= rl.cleanupInterval {
		for ip, v := range rl.visitors {
			if now.Sub(v.lastSeen) > rl.entryTTL {
				delete(rl.visitors, ip)
			}
		}
		rl.lastCleanup = now
	}

	v, exists := rl.visitors[ip]
	if !exists {
		limiter := rate.NewLimiter(rl.rate, rl.burst)
		rl.visitors[ip] = &visitor{limiter: limiter, lastSeen: now}
		return limiter
	}

	v.lastSeen = now
	return v.limiter
}

func RateLimit(r, b int) func(http.Handler) http.Handler {
	return NewRateLimiter(r, b).Middleware()
}

func MaxRequestSize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
