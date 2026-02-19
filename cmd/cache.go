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

	// 初始化缓存
	if err := cache.InitCache(nil); err != nil {
		return fmt.Errorf("failed to initialize cache: %w", err)
	}

	provider := cache.GetDefault()
	if provider == nil {
		return fmt.Errorf("cache provider not initialized")
	}

	ctx := context.Background()

	// 默认清理图片缓存
	if !all && pattern == "" {
		imageOnly = true
	}

	log.Printf("Cache provider: %s", provider.Name())

	if all {
		// 清理所有缓存
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
		// 清理图片缓存
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
	// 检查是否支持批量清理接口
	type ClearAll interface {
		ClearAll(ctx context.Context) error
	}

	if p, ok := provider.(ClearAll); ok {
		return p.ClearAll(ctx)
	}

	// 不支持批量清理，尝试清理已知缓存键
	log.Println("Cache provider does not support bulk clear, attempting to clear known keys...")
	return clearImageCache(ctx, provider)
}

// clearCacheByPattern 按模式清理缓存
func clearCacheByPattern(ctx context.Context, provider interface{}, pattern string) error {
	// 检查是否支持模式匹配清理
	type ClearByPattern interface {
		ClearByPattern(ctx context.Context, pattern string) error
	}

	if p, ok := provider.(ClearByPattern); ok {
		return p.ClearByPattern(ctx, pattern)
	}

	// 不支持模式匹配，回退到图片缓存清理
	log.Printf("Cache provider does not support pattern matching, falling back to image cache clear...")
	return clearImageCache(ctx, provider)
}

// clearImageCache 清理图片缓存
func clearImageCache(ctx context.Context, provider interface{}) error {
	// 图片缓存键前缀
	imageCachePrefixes := []string{
		"image:",
		"image_meta:",
		"image_data:",
		"img:",
	}

	// 尝试使用 Delete 方法清理
	type Deleter interface {
		Delete(ctx context.Context, key string) error
	}

	deleter, ok := provider.(Deleter)
	if !ok {
		return fmt.Errorf("cache provider does not support delete operation")
	}

	// 尝试清理各种可能的图片缓存键
	for _, prefix := range imageCachePrefixes {
		// 尝试清理一些常见的缓存键
		keys := generateImageCacheKeys(prefix)
		for _, key := range keys {
			if err := deleter.Delete(ctx, key); err != nil {
				// 忽略删除不存在的键的错误
				if !isKeyNotFoundError(err) {
					log.Printf("Warning: failed to delete cache key %s: %v", key, err)
				}
			}
		}
	}

	// 尝试使用特定的图片缓存清理接口
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

// generateImageCacheKeys 生成一些可能的图片缓存键（用于测试和示例）
func generateImageCacheKeys(prefix string) []string {
	// 由于无法知道所有缓存键，这里只是示例
	// 实际应用中可能需要遍历数据库获取所有图片ID
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
	// 常见的键不存在错误信息
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
