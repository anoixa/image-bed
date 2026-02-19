package core

import (
	"context"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/storage"
)

func checkDatabaseHealth(provider database.Provider) string {
	if provider == nil {
		return "not initialized"
	}

	db := provider.DB()
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

func checkCacheHealth() string {
	if cache.GetDefault() != nil {
		return "ok"
	}
	return "not initialized"
}

func checkStorageHealth() string {
	provider := storage.GetDefault()
	if provider == nil {
		return "error: no default storage provider"
	}

	ctx := context.Background()
	if err := provider.Health(ctx); err != nil {
		return "error: " + err.Error()
	}

	return "ok"
}
