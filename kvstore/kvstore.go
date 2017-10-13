package kvstore

import (
	"fmt"
	"time"

	"github.com/admpub/gokvstores"
	"github.com/admpub/picfit/config"
)

// NewKVStoreFromConfig returns a KVStore from config
func NewKVStoreFromConfig(cfg *config.Config) (gokvstores.KVStore, error) {
	if cfg.KVStore == nil {
		return &gokvstores.DummyStore{}, nil
	}

	section := cfg.KVStore

	switch section.Type {
	case "dummy":
		return &gokvstores.DummyStore{}, nil

	case "redis":
		return gokvstores.NewRedisClientStore(&gokvstores.RedisClientOptions{
			Addr:     fmt.Sprintf("%s:%d", section.Host, section.Port),
			Password: section.Password,
			DB:       section.Db,
		}, time.Second*30)

	case "cache":
		if section.Expiration == 0 {
			section.Expiration = 86400 * 365
		}
		if section.CleanupInterval == 0 {
			section.CleanupInterval = 86400 * 365
		}
		return gokvstores.NewMemoryStore(section.Expiration, section.CleanupInterval)

	}

	return nil, fmt.Errorf("kvstore %s does not exist", section.Type)
}
