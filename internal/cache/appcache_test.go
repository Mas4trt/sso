package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"sso/internal/cache"
	"sso/internal/domain/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingLookup struct {
	calls int
	app   models.App
	err   error
}

func (l *countingLookup) App(_ context.Context, _ uint64) (models.App, error) {
	l.calls++
	return l.app, l.err
}

func TestAppCache_HitsDontReachBackend(t *testing.T) {
	backend := &countingLookup{app: models.App{ID: 1, Secret: "s1"}}
	c := cache.NewAppCache(backend, time.Minute, time.Second)

	for i := 0; i < 5; i++ {
		app, err := c.App(context.Background(), 1)
		require.NoError(t, err)
		assert.Equal(t, "s1", app.Secret)
	}

	assert.Equal(t, 1, backend.calls, "expected exactly one backend call, rest should be served from cache")
}

func TestAppCache_ExpiresAfterTTL(t *testing.T) {
	backend := &countingLookup{app: models.App{ID: 1, Secret: "s1"}}
	c := cache.NewAppCache(backend, 10*time.Millisecond, 10*time.Millisecond)

	_, err := c.App(context.Background(), 1)
	require.NoError(t, err)

	time.Sleep(20 * time.Millisecond)

	_, err = c.App(context.Background(), 1)
	require.NoError(t, err)

	assert.Equal(t, 2, backend.calls)
}

func TestAppCache_NegativeResultsAreCachedSeparately(t *testing.T) {
	wantErr := errors.New("app not found")
	backend := &countingLookup{err: wantErr}
	c := cache.NewAppCache(backend, time.Minute, time.Hour)

	_, err1 := c.App(context.Background(), 999)
	_, err2 := c.App(context.Background(), 999)

	assert.ErrorIs(t, err1, wantErr)
	assert.ErrorIs(t, err2, wantErr)
	assert.Equal(t, 1, backend.calls, "negative result should also be cached")
}

func TestAppCache_InvalidateForcesRefetch(t *testing.T) {
	backend := &countingLookup{app: models.App{ID: 1, Secret: "s1"}}
	c := cache.NewAppCache(backend, time.Minute, time.Minute)

	_, _ = c.App(context.Background(), 1)
	c.Invalidate(1)
	_, _ = c.App(context.Background(), 1)

	assert.Equal(t, 2, backend.calls)
}
