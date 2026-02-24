package cmd

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/spf13/cobra"
)

// cacheCmd 缓存管理命令
var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Cache management commands",
	Long:  "Manage application cache, including clearing image cache.",
}

// cacheClearCmd 清除缓存命令
var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear cache",
	Long:  `Clear application cache. By default clears all image cache.`,
	Run: func(cmd *cobra.Command, args []string) {
		imageOnly, _ := cmd.Flags().GetBool("image-only")
		all, _ := cmd.Flags().GetBool("all")
		pattern, _ := cmd.Flags().GetString("pattern")

		if err := runCacheClear(imageOnly, all, pattern); err != nil {
			log.Fatalf("Cache clear failed: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cacheClearCmd)

	cacheClearCmd.Flags().Bool("image-only", false, "Only clear image cache")
	cacheClearCmd.Flags().Bool("all", false, "Clear all cache")
	cacheClearCmd.Flags().String("pattern", "", "Clear cache keys matching pattern (e.g., 'image:*')")
}

// runCacheClear 执行缓存清理
func runCacheClear(imageOnly, all bool, pattern string) error {
	config.InitConfig()

	if err := cache.InitDefault(); err != nil {
		return fmt.Errorf("failed to initialize cache: %w", err)
	}

	provider := cache.GetDefault()
	if provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	ctx := context.Background()

	if !all && pattern == "" {
		imageOnly = true
	}

	log.Printf("Cache provider: %s", provider.Name())

	if all {
		log.Println("Clearing all cache...")
		if err := clearAllCache(ctx, provider); err != nil {
			return fmt.Errorf("failed to clear all cache: %w", err)
		}
		log.Println("All cache cleared successfully")
	} else if pattern != "" {
		// 按模式清理
		log.Printf("Clearing cache matching pattern: %s", pattern)
		if err := clearCacheByPattern(ctx, provider, pattern); err != nil {
			return fmt.Errorf("failed to clear cache by pattern: %w", err)
		}
		log.Printf("Cache matching pattern '%s' cleared successfully", pattern)
	} else if imageOnly {
		log.Println("Clearing image cache...")
		if err := clearImageCache(ctx, provider); err != nil {
			return fmt.Errorf("failed to clear image cache: %w", err)
		}
		log.Println("Image cache cleared successfully")
	}

	return nil
}

// clearAllCache 清理所有缓存
func clearAllCache(ctx context.Context, provider interface{}) error {
	type ClearAll interface {
		ClearAll(ctx context.Context) error
	}

	if p, ok := provider.(ClearAll); ok {
		return p.ClearAll(ctx)
	}

	log.Println("Cache provider does not support bulk clear, attempting to clear known keys...")
	return clearImageCache(ctx, provider)
}

// clearCacheByPattern 按模式清理缓存
func clearCacheByPattern(ctx context.Context, provider interface{}, pattern string) error {
	type ClearByPattern interface {
		ClearByPattern(ctx context.Context, pattern string) error
	}

	if p, ok := provider.(ClearByPattern); ok {
		return p.ClearByPattern(ctx, pattern)
	}

	log.Printf("Cache provider does not support pattern matching, falling back to image cache clear...")
	return clearImageCache(ctx, provider)
}

// clearImageCache 清理图片缓存
func clearImageCache(ctx context.Context, provider interface{}) error {
	imageCachePrefixes := []string{
		"image:",
		"image_meta:",
		"image_data:",
		"img:",
	}

	type Deleter interface {
		Delete(ctx context.Context, key string) error
	}

	deleter, ok := provider.(Deleter)
	if !ok {
		return fmt.Errorf("cache provider does not support delete operation")
	}

	for _, prefix := range imageCachePrefixes {
		keys := generateImageCacheKeys(prefix)
		for _, key := range keys {
			if err := deleter.Delete(ctx, key); err != nil {
				if !isKeyNotFoundError(err) {
					log.Printf("Warning: failed to delete cache key %s: %v", key, err)
				}
			}
		}
	}

	type ImageCacheClear interface {
		ClearImageCache(ctx context.Context) error
	}

	if p, ok := provider.(ImageCacheClear); ok {
		if err := p.ClearImageCache(ctx); err != nil {
			return err
		}
	}

	return nil
}

// generateImageCacheKeys 生成一些可能的图片缓存键
func generateImageCacheKeys(prefix string) []string {
	return []string{
		prefix + "list",
		prefix + "all",
	}
}

// isKeyNotFoundError 检查是否是键不存在的错误
func isKeyNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	notFoundPatterns := []string{
		"not found",
		"does not exist",
		"key not found",
		"cache miss",
	}

	errStr := err.Error()
	for _, pattern := range notFoundPatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}
	return false
}
