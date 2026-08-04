package cache

import (
	"context"
	"sync"
	"time"

	"sso/internal/domain/models"
)

// AppLookup is satisfied by postgres.Storage and matches both
// interceptors.AppSecretLookup and services/auth.AppProvider, so a single
// cache instance can back both call sites.
type AppLookup interface {
	App(ctx context.Context, appID uint64) (models.App, error)
}

type entry struct {
	app       models.App
	err       error
	expiresAt time.Time
}

// AppCache wraps an AppLookup with a bounded-TTL read-through cache.
//
// Why this exists: every authenticated RPC (GetRole today, more later)
// resolves the calling app's HMAC secret to verify the access token's
// signature. Without caching that's one extra postgres round trip per
// request just to read a value that changes on the order of "never, unless
// an operator rotates it." App secrets are also small in number (one row
// per consuming service) and rarely rotated, which makes them a good fit
// for a short-TTL cache rather than invalidation plumbing.
//
// Negative results (ErrAppNotFound) are cached too, with a shorter TTL, so
// a misconfigured or malicious client repeatedly sending a bogus
// application_id can't turn into a postgres query storm.
type AppCache struct {
	next AppLookup

	mu      sync.RWMutex
	entries map[uint64]entry

	ttl    time.Duration
	negTTL time.Duration
	now    func() time.Time
}

const (
	DefaultTTL    = 5 * time.Minute
	DefaultNegTTL = 30 * time.Second
)

func NewAppCache(next AppLookup, ttl, negTTL time.Duration) *AppCache {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if negTTL <= 0 {
		negTTL = DefaultNegTTL
	}
	return &AppCache{
		next:    next,
		entries: make(map[uint64]entry),
		ttl:     ttl,
		negTTL:  negTTL,
		now:     time.Now,
	}
}

func (c *AppCache) App(ctx context.Context, appID uint64) (models.App, error) {
	if cached, ok := c.lookup(appID); ok {
		return cached.app, cached.err
	}

	app, err := c.next.App(ctx, appID)

	ttl := c.ttl
	if err != nil {
		ttl = c.negTTL
	}

	c.mu.Lock()
	c.entries[appID] = entry{app: app, err: err, expiresAt: c.now().Add(ttl)}
	c.mu.Unlock()

	return app, err
}

func (c *AppCache) lookup(appID uint64) (entry, bool) {
	c.mu.RLock()
	e, ok := c.entries[appID]
	c.mu.RUnlock()

	if !ok || c.now().After(e.expiresAt) {
		return entry{}, false
	}
	return e, true
}

// Invalidate drops a cached entry immediately. Call this from any future
// "rotate app secret" / "delete app" admin path so a rotation takes effect
// without waiting out the TTL.
func (c *AppCache) Invalidate(appID uint64) {
	c.mu.Lock()
	delete(c.entries, appID)
	c.mu.Unlock()
}
