package lru

import (
	"container/list"
)

type (
	LRU[K comparable, T any] struct {
		capacity int
		cache    map[K]*list.Element
		list     *list.List
	}
	entity[K comparable, T any] struct {
		key   K
		value T
	}
)

func New[K comparable, T any](capacity int) *LRU[K, T] {
	return &LRU[K, T]{
		capacity: capacity,
		cache:    make(map[K]*list.Element),
		list:     list.New(),
	}
}

func (c *LRU[K, T]) Put(key K, value T) {
	if elem, ok := c.cache[key]; ok {
		elem.Value.(*entity[K, T]).value = value
		c.list.MoveToFront(elem)
		return
	}
	c.cache[key] = c.list.PushFront(&entity[K, T]{key: key, value: value})

	if c.list.Len() > c.capacity {
		elem := c.list.Back()
		if elem != nil {
			c.list.Remove(elem)
			delete(c.cache, elem.Value.(*entity[K, T]).key)
		}
	}
}

func (c *LRU[K, T]) Exist(key K) bool {
	_, ok := c.cache[key]
	return ok
}

func (c *LRU[K, T]) Get(key K) (value T, exist bool) {
	if elem, ok := c.cache[key]; ok {
		c.list.MoveToFront(elem)
		value = elem.Value.(*entity[K, T]).value
		exist = true
	}
	return value, exist
}
