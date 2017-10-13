package kvstore

import (
	"github.com/admpub/gokvstores"
)

type DummyKVStore struct {
}

func (k *DummyKVStore) Connection() gokvstores.KVStore {
	return &gokvstores.DummyStore{}
}

func (k *DummyKVStore) Close() error {
	return nil
}
