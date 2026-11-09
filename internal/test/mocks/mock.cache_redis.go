package mocks

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-rest-boilerplate/internal/datasources/caches"
)

type RedisCache struct {
	caches.RedisCache
	server *miniredis.Miniredis
}

var _ caches.RedisCache = (*RedisCache)(nil)

func NewRedisCache(t testing.TB) *RedisCache {
	t.Helper()

	srv := miniredis.RunT(t)

	rc := caches.NewRedisCache(srv.Addr(), 0, "", time.Duration(5))
	t.Cleanup(func() { _ = rc.Close() })

	return &RedisCache{RedisCache: rc, server: srv}
}

func (r *RedisCache) Server() *miniredis.Miniredis { return r.server }

func (r *RedisCache) FastForward(d time.Duration) { r.server.FastForward(d) }