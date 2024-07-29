package lru

import (
	"testing"
)

func TestNewLRU(t *testing.T) {
	cache := New[int, int](2)
	cache.Put(1, 1)
	cache.Put(2, 2)
	t.Log(cache.Get(1))
	cache.Put(3, 3)
	t.Log(cache.Get(2))
	t.Log(cache.Get(3))
}
