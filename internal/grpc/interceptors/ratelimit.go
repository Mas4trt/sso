package interceptors

import (
	"context"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

type MethodRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	methods map[string]bool
	rate    float64
	burst   float64
	ttl     time.Duration
}

func NewMethodRateLimiter(rate, burst float64, ttl time.Duration, methods ...string) *MethodRateLimiter {
	set := make(map[string]bool, len(methods))
	for _, m := range methods {
		set[m] = true
	}
	l := &MethodRateLimiter{
		buckets: make(map[string]*bucket),
		methods: set,
		rate:    rate,
		burst:   burst,
		ttl:     ttl,
	}
	go l.sweep()
	return l
}

func (l *MethodRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, lastSeen: now}
		l.buckets[key] = b
	}

	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *MethodRateLimiter) sweep() {
	ticker := time.NewTicker(l.ttl)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-l.ttl)
		l.mu.Lock()
		for key, b := range l.buckets {
			if b.lastSeen.Before(cutoff) {
				delete(l.buckets, key)
			}
		}
		l.mu.Unlock()
	}
}

func (l *MethodRateLimiter) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if !l.methods[info.FullMethod] {
			return handler(ctx, req)
		}
		if !l.allow(peerIP(ctx)) {
			return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded")
		}
		return handler(ctx, req)
	}
}

func peerIP(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(p.Addr.String())
	if err != nil {
		return p.Addr.String()
	}
	return host
}
