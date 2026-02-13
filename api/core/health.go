package core

import (
	"context"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/internal/repositories"
	"github.com/anoixa/image-bed/storage"
)

func checkDatabaseHealth(repos *repositories.Repositories) string {
	if repos == nil {
		return "not initialized"
	}

	db := repos.Accounts.DB()
	if db == nil {
		return "not initialized"
	}
	sqlDB, err := db.DB()
	if err != nil {
		return "error: " + err.Error()
	}
	if err := sqlDB.Ping(); err != nil {
		return "unavailable: " + err.Error()
	}
	return "ok"
}

func checkCacheHealth(cacheFactory *cache.Factory) string {
	if cacheFactory == nil {
		return "not initialized"
	}
	if cacheFactory.GetProvider() != nil {
		return "ok"
	}
	return "not initialized"
}

func checkStorageHealth(storageFactory *storage.Factory) string {
	if storageFactory == nil {
		return "not initialized"
	}

	provider, err := storageFactory.Get(config.Get().Server.StorageConfig.Type)
	if err != nil {
		return "error: " + err.Error()
	}

	ctx := context.Background()
	if err := provider.Health(ctx); err != nil {
		return "error: " + err.Error()
	}

	return "ok"
}
