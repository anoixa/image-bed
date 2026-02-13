package cache

import (
	"context"
	"testing"
	"time"

	"github.com/anoixa/image-bed/cache/ristretto"
)

func TestRistrettoCache(t *testing.T) {
	config := ristretto.Config{
		NumCounters: 1000,
		MaxCost:     1000,
		BufferItems: 64,
		Metrics:     false,
	}

	cache, err := ristretto.NewRistretto(config)
	if err != nil {
		t.Fatalf("Failed to create ristretto cache: %v", err)
	}

	ctx := context.Background()
	key := "test_key"
	value := "test_value"
	expiration := 10 * time.Second

	err = cache.Set(ctx, key, value, expiration)
	if err != nil {
		t.Fatalf("Failed to set cache value: %v", err)
	}

	var retrievedValue string
	err = cache.Get(ctx, key, &retrievedValue)
	if err != nil {
		t.Fatalf("Failed to get cache value: %v", err)
	}

	if retrievedValue != value {
		t.Errorf("Retrieved value %s does not match original value %s", retrievedValue, value)
	}

	// 测试Exists
	exists, err := cache.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Failed to check if key exists: %v", err)
	}
	if !exists {
		t.Error("Key should exist but was not found")
	}

	// 测试Delete
	err = cache.Delete(ctx, key)
	if err != nil {
		t.Fatalf("Failed to delete cache key: %v", err)
	}

	// 再次获取应该返回错误
	err = cache.Get(ctx, key, &retrievedValue)
	if err == nil {
		t.Error("Should return error for deleted key")
	}
}

func TestRistrettoCacheStruct(t *testing.T) {
	config := ristretto.Config{
		NumCounters: 1000,
		MaxCost:     1000,
		BufferItems: 64,
		Metrics:     false,
	}

	cache, err := ristretto.NewRistretto(config)
	if err != nil {
		t.Fatalf("Failed to create ristretto cache: %v", err)
	}

	ctx := context.Background()

	type TestStruct struct {
		Name  string
		Value int
	}

	key := "struct_key"
	value := TestStruct{Name: "test", Value: 42}
	expiration := 10 * time.Second

	err = cache.Set(ctx, key, value, expiration)
	if err != nil {
		t.Fatalf("Failed to set cache value: %v", err)
	}

	var retrievedValue TestStruct
	err = cache.Get(ctx, key, &retrievedValue)
	if err != nil {
		t.Fatalf("Failed to get cache value: %v", err)
	}

	if retrievedValue.Name != value.Name || retrievedValue.Value != value.Value {
		t.Errorf("Retrieved value %+v does not match original value %+v", retrievedValue, value)
	}
}

func TestCacheMiss(t *testing.T) {
	config := ristretto.Config{
		NumCounters: 1000,
		MaxCost:     1000,
		BufferItems: 64,
		Metrics:     false,
	}

	cache, err := ristretto.NewRistretto(config)
	if err != nil {
		t.Fatalf("Failed to create ristretto cache: %v", err)
	}

	ctx := context.Background()

	// 尝试获取不存在的key
	var value string
	err = cache.Get(ctx, "nonexistent_key", &value)
	if err == nil {
		t.Error("Should return error for nonexistent key")
	}

	// 使用 ristretto 包中的 ErrCacheMiss 检查
	if err != ristretto.ErrCacheMiss {
		t.Errorf("Error should be cache miss, got: %v", err)
	}
}
