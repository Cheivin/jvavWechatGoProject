package db

import (
	"errors"
	"github.com/dgraph-io/badger/v4"
	"time"
)

type BadgerStorage struct {
	db *badger.DB
}

func NewBadgerStorage(dirname string) (Storage, error) {
	db, err := badger.Open(badger.DefaultOptions(dirname))
	if err != nil {
		return nil, err
	}
	return &BadgerStorage{db: db}, nil
}

// PutIfAbsent 如果没有则放入value，并返回true，否则返回false
func (s BadgerStorage) PutIfAbsent(key string, value string) bool {
	put := false
	_ = s.db.Update(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(key))
		if err == nil {
			return err
		} else if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		} else {
			put = true
			return txn.Set([]byte(key), []byte(value))
		}
	})
	return put
}

// PutIfAbsentWithTTL 如果没有则放入value，并返回true，否则返回false
func (s BadgerStorage) PutIfAbsentWithTTL(key string, value string, ttl time.Duration) bool {
	put := false
	_ = s.db.Update(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(key))
		if err == nil {
			return err
		} else if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		} else {
			put = true
			return txn.SetEntry(badger.NewEntry([]byte(key), []byte(value)).WithTTL(ttl))
		}
	})
	return put
}
func (s BadgerStorage) Get(key string) (string, error) {
	var val []byte
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err == nil {
			val, err = item.ValueCopy(nil)
			return err
		} else if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		} else {
			return err
		}
	})
	return string(val), err
}

func (s BadgerStorage) Save(key string, value string) error {
	wb := s.db.NewWriteBatch()
	defer wb.Cancel()
	if err := wb.Set([]byte(key), []byte(value)); err != nil {
		return err
	}
	return wb.Flush()
}

func (s BadgerStorage) SaveWithTTL(key string, value string, ttl time.Duration) error {
	wb := s.db.NewWriteBatch()
	defer wb.Cancel()
	if err := wb.SetEntry(badger.NewEntry([]byte(key), []byte(value)).WithTTL(ttl)); err != nil {
		return err
	}
	return wb.Flush()
}
func (s BadgerStorage) SaveKeys(keys []string, value string) error {
	wb := s.db.NewWriteBatch()
	valueBytes := []byte(value)
	defer wb.Cancel()
	for _, key := range keys {
		if err := wb.Set([]byte(key), valueBytes); err != nil {
			return err
		}
	}
	return wb.Flush()
}

func (s BadgerStorage) SaveKeysWithTTL(keys []string, value string, ttl time.Duration) error {
	wb := s.db.NewWriteBatch()
	valueBytes := []byte(value)
	defer wb.Cancel()
	for _, key := range keys {
		if err := wb.SetEntry(badger.NewEntry([]byte(key), valueBytes).WithTTL(ttl)); err != nil {
			return err
		}
	}
	return wb.Flush()
}
func (s BadgerStorage) GetOrSaveKeys(keys []string, fn func(keys []string) (string, time.Duration)) (string, error) {
	for i, key := range keys {
		data, err := s.Get(key)
		if err != nil {
			return "", err
		}
		if data != "" {
			if i != 0 {
				_ = s.SaveKeys(keys, data)
			}
			return data, nil
		}
	}
	id, ttl := fn(keys)
	if ttl <= 0 {
		return id, s.SaveKeys(keys, id)
	}
	return id, s.SaveKeysWithTTL(keys, id, ttl)
}
