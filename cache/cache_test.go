package cache

import (
	"testing"
	"time"

	"github.com/anoixa/image-bed/cache/ristretto"
	"github.com/anoixa/image-bed/cache/types"
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

	key := "test_key"
	value := "test_value"
	expiration := 10 * time.Second

	err = cache.Set(key, value, expiration)
	if err != nil {
		t.Fatalf("Failed to set cache value: %v", err)
	}

	var retrievedValue string
	err = cache.Get(key, &retrievedValue)
	if err != nil {
		t.Fatalf("Failed to get cache value: %v", err)
	}

	if retrievedValue != value {
		t.Errorf("Retrieved value %s does not match original value %s", retrievedValue, value)
	}

	// 测试Exists
	exists, err := cache.Exists(key)
	if err != nil {
		t.Fatalf("Failed to check if key exists: %v", err)
	}
	if !exists {
		t.Error("Key should exist but was not found")
	}

	// 测试Delete
	err = cache.Delete(key)
	if err != nil {
		t.Fatalf("Failed to delete cache key: %v", err)
	}

	// 确认键已被删除
	err = cache.Get(key, &retrievedValue)
	if err == nil {
		t.Error("Expected error when getting deleted key, but got none")
	}
	if !types.IsCacheMiss(err) {
		t.Errorf("Expected cache miss error, but got: %v", err)
	}

	// 测试Exists对已删除键的检查
	exists, err = cache.Exists(key)
	if err != nil {
		t.Fatalf("Failed to check if deleted key exists: %v", err)
	}
	if exists {
		t.Error("Deleted key should not exist")
	}

	// 测试Close
	err = cache.Close()
	if err != nil {
		t.Fatalf("Failed to close cache: %v", err)
	}
}

func TestCacheManagerWithRistretto(t *testing.T) {
	// 创建缓存管理器配置
	config := Config{
		Provider: "memory",
		Ristretto: RistrettoConfig{
			NumCounters: 1000,
			MaxCost:     1000,
			BufferItems: 64,
			Metrics:     false,
		},
	}

	// 创建缓存管理器
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("Failed to create cache manager: %v", err)
	}

	// 测试Set和Get
	key := "manager_test_key"
	value := map[string]interface{}{
		"name": "test",
		"age":  float64(30),
	}

	expiration := 10 * time.Second

	err = manager.Set(key, value, expiration)
	if err != nil {
		t.Fatalf("Failed to set cache value through manager: %v", err)
	}

	var retrievedValue map[string]interface{}
	err = manager.Get(key, &retrievedValue)
	if err != nil {
		t.Fatalf("Failed to get cache value through manager: %v", err)
	}

	if retrievedValue["name"] != value["name"] {
		t.Errorf("Retrieved name %s does not match original name %s", retrievedValue["name"], value["name"])
	}

	if retrievedValue["age"] != value["age"] {
		t.Errorf("Retrieved age %v does not match original age %v", retrievedValue["age"], value["age"])
	}

	// 测试Delete
	err = manager.Delete(key)
	if err != nil {
		t.Fatalf("Failed to delete cache key through manager: %v", err)
	}

	// 测试Close
	err = manager.Close()
	if err != nil {
		t.Fatalf("Failed to close cache manager: %v", err)
	}
}
