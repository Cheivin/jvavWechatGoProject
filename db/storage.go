package db

import "time"

type Storage interface {
	Get(key string) (string, error)
	Save(key string, value string) error
	SaveWithTTL(key string, value string, ttl time.Duration) error
	SaveKeys(keys []string, value string) error
	SaveKeysWithTTL(keys []string, value string, ttl time.Duration) error
	PutIfAbsent(key string, value string) bool
	PutIfAbsentWithTTL(key string, value string, ttl time.Duration) bool
	GetOrSaveKeys(keys []string, fn func(keys []string) (string, time.Duration)) (string, error)
}
