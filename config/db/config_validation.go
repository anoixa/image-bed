package config

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/anoixa/image-bed/database/models"
)

var (
	storageCommonKeys = keySet("type")
	localStorageKeys  = mergeKeySets(storageCommonKeys, keySet("local_path"))
	s3StorageKeys     = mergeKeySets(storageCommonKeys, keySet(
		"endpoint",
		"region",
		"bucket_name",
		"access_key_id",
		"secret_access_key",
		"force_path_style",
		"public_domain",
		"is_private",
	))
	webDAVStorageKeys = mergeKeySets(storageCommonKeys, keySet(
		"webdav_url",
		"webdav_username",
		"webdav_password",
		"webdav_root_path",
	))

	imageProcessingKeys = keySet(
		"thumbnail_enabled",
		"thumbnail_sizes",
		"thumbnail_quality",
		"conversion_enabled_formats",
		"webp_quality",
		"webp_effort",
		"avif_quality",
		"avif_speed",
		"avif_experimental",
		"skip_smaller_than",
		"max_dimension",
		"default_album_id",
		"default_visibility",
		"concurrent_upload_limit",
		"max_file_size_mb",
		"max_batch_total_mb",
		"api_key_enabled",
	)

	securityKeys = keySet("password_login_enabled")
)

const (
	maxInt64Value = int64(^uint64(0) >> 1)
	maxIntValue   = int64(^uint(0) >> 1)
	minIntValue   = -maxIntValue - 1
)

// ValidateSystemConfigMap strictly validates the decrypted/plain config map for
// categories that are accepted through the dynamic config boundary. It rejects
// unknown keys and wrong JSON types before encryption so corrupted config cannot
// be persisted and then fail later during startup or request handling.
func ValidateSystemConfigMap(category models.ConfigCategory, configMap map[string]any) error {
	if configMap == nil {
		return invalidConfig("config is required")
	}

	switch category {
	case models.ConfigCategoryStorage:
		return validateStorageConfigMap(configMap)
	case models.ConfigCategoryOAuth:
		return ValidateOAuthConfigMap(configMap)
	case models.ConfigCategoryImageProcessing:
		return validateImageProcessingConfigMap(configMap)
	case models.ConfigCategorySecurity:
		if err := rejectUnknownKeys(configMap, securityKeys); err != nil {
			return err
		}
		if _, err := parseAuthSettings(configMap); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
		}
		return nil
	case models.ConfigCategorySystem:
		return nil
	case models.ConfigCategoryJWT:
		return invalidConfig("jwt config is not managed through the dynamic config API")
	default:
		return invalidConfig("unsupported config category %q", category)
	}
}

func validateStorageConfigMap(configMap map[string]any) error {
	storageType, err := requiredString(configMap, "type")
	if err != nil {
		return err
	}

	switch storageType {
	case "local":
		if err := rejectUnknownKeys(configMap, localStorageKeys); err != nil {
			return err
		}
		_, err := requiredString(configMap, "local_path")
		return err
	case "s3":
		if err := rejectUnknownKeys(configMap, s3StorageKeys); err != nil {
			return err
		}
		for _, key := range []string{"endpoint", "bucket_name", "access_key_id", "secret_access_key"} {
			if _, err := requiredString(configMap, key); err != nil {
				return err
			}
		}
		for _, key := range []string{"region", "public_domain"} {
			if err := optionalString(configMap, key); err != nil {
				return err
			}
		}
		for _, key := range []string{"force_path_style", "is_private"} {
			if err := optionalBool(configMap, key); err != nil {
				return err
			}
		}
		return nil
	case "webdav":
		if err := rejectUnknownKeys(configMap, webDAVStorageKeys); err != nil {
			return err
		}
		if _, err := requiredString(configMap, "webdav_url"); err != nil {
			return err
		}
		for _, key := range []string{"webdav_username", "webdav_password", "webdav_root_path"} {
			if err := optionalString(configMap, key); err != nil {
				return err
			}
		}
		return nil
	default:
		return invalidConfig("unsupported storage type %q", storageType)
	}
}

func validateImageProcessingConfigMap(configMap map[string]any) error {
	if err := rejectUnknownKeys(configMap, imageProcessingKeys); err != nil {
		return err
	}

	settings := &ImageProcessingSettings{}

	var err error
	if settings.ThumbnailEnabled, err = requiredBool(configMap, "thumbnail_enabled"); err != nil {
		return err
	}
	if settings.ThumbnailSizes, err = requiredThumbnailSizes(configMap, "thumbnail_sizes"); err != nil {
		return err
	}
	if settings.ThumbnailQuality, err = requiredInt(configMap, "thumbnail_quality"); err != nil {
		return err
	}
	if settings.ConversionEnabledFormats, err = requiredStringSlice(configMap, "conversion_enabled_formats"); err != nil {
		return err
	}
	if settings.WebPQuality, err = requiredInt(configMap, "webp_quality"); err != nil {
		return err
	}
	if settings.WebPEffort, err = requiredInt(configMap, "webp_effort"); err != nil {
		return err
	}
	if settings.AVIFQuality, err = requiredInt(configMap, "avif_quality"); err != nil {
		return err
	}
	if settings.AVIFSpeed, err = requiredInt(configMap, "avif_speed"); err != nil {
		return err
	}
	if settings.AVIFExperimental, err = requiredBool(configMap, "avif_experimental"); err != nil {
		return err
	}
	if settings.SkipSmallerThan, err = requiredInt(configMap, "skip_smaller_than"); err != nil {
		return err
	}
	if settings.MaxDimension, err = requiredInt(configMap, "max_dimension"); err != nil {
		return err
	}
	defaultAlbumID, err := requiredUint(configMap, "default_album_id")
	if err != nil {
		return err
	}
	settings.DefaultAlbumID = defaultAlbumID
	if settings.DefaultVisibility, err = requiredString(configMap, "default_visibility"); err != nil {
		return err
	}
	if settings.ConcurrentUploadLimit, err = requiredInt(configMap, "concurrent_upload_limit"); err != nil {
		return err
	}
	if settings.MaxFileSizeMB, err = requiredInt(configMap, "max_file_size_mb"); err != nil {
		return err
	}
	if settings.MaxBatchTotalMB, err = requiredInt(configMap, "max_batch_total_mb"); err != nil {
		return err
	}
	if settings.APIKeyEnabled, err = requiredBool(configMap, "api_key_enabled"); err != nil {
		return err
	}

	for _, format := range settings.ConversionEnabledFormats {
		switch format {
		case models.FormatWebP, models.FormatAVIF:
		default:
			return invalidConfig("conversion_enabled_formats contains unsupported format %q", format)
		}
	}
	if settings.MaxDimension < 0 {
		return invalidConfig("max_dimension must be non-negative")
	}
	if settings.SkipSmallerThan < 0 {
		return invalidConfig("skip_smaller_than must be non-negative")
	}
	if err := settings.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	return nil
}

func requiredString(m map[string]any, key string) (string, error) {
	raw, ok := m[key]
	if !ok {
		return "", invalidConfig("%s is required", key)
	}
	value, ok := raw.(string)
	if !ok {
		return "", invalidConfig("%s must be a string, got %T", key, raw)
	}
	if strings.TrimSpace(value) == "" {
		return "", invalidConfig("%s is required", key)
	}
	return value, nil
}

func optionalString(m map[string]any, key string) error {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	if _, ok := raw.(string); !ok {
		return invalidConfig("%s must be a string, got %T", key, raw)
	}
	return nil
}

func requiredBool(m map[string]any, key string) (bool, error) {
	raw, ok := m[key]
	if !ok {
		return false, invalidConfig("%s is required", key)
	}
	value, ok := raw.(bool)
	if !ok {
		return false, invalidConfig("%s must be a bool, got %T", key, raw)
	}
	return value, nil
}

func optionalBool(m map[string]any, key string) error {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	if _, ok := raw.(bool); !ok {
		return invalidConfig("%s must be a bool, got %T", key, raw)
	}
	return nil
}

func requiredInt(m map[string]any, key string) (int, error) {
	raw, ok := m[key]
	if !ok {
		return 0, invalidConfig("%s is required", key)
	}
	n, err := int64FromAny(raw, key)
	if err != nil {
		return 0, err
	}
	if n < minIntValue || n > maxIntValue {
		return 0, invalidConfig("%s is outside int range", key)
	}
	return int(n), nil
}

func requiredUint(m map[string]any, key string) (uint, error) {
	raw, ok := m[key]
	if !ok {
		return 0, invalidConfig("%s is required", key)
	}
	n, err := int64FromAny(raw, key)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, invalidConfig("%s must be non-negative", key)
	}
	return uint(n), nil
}

func requiredStringSlice(m map[string]any, key string) ([]string, error) {
	raw, ok := m[key]
	if !ok {
		return nil, invalidConfig("%s is required", key)
	}
	switch values := raw.(type) {
	case []string:
		out := append([]string(nil), values...)
		return validateStringSlice(key, out)
	case []any:
		out := make([]string, 0, len(values))
		for i, item := range values {
			text, ok := item.(string)
			if !ok {
				return nil, invalidConfig("%s[%d] must be a string, got %T", key, i, item)
			}
			out = append(out, text)
		}
		return validateStringSlice(key, out)
	default:
		return nil, invalidConfig("%s must be an array of strings, got %T", key, raw)
	}
}

func validateStringSlice(key string, values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, invalidConfig("%s must not be empty", key)
	}
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, invalidConfig("%s[%d] must not be empty", key, i)
		}
	}
	return values, nil
}

func requiredThumbnailSizes(m map[string]any, key string) ([]models.ThumbnailSize, error) {
	raw, ok := m[key]
	if !ok {
		return nil, invalidConfig("%s is required", key)
	}

	var sizes []models.ThumbnailSize
	switch values := raw.(type) {
	case []models.ThumbnailSize:
		sizes = append([]models.ThumbnailSize(nil), values...)
	case []any:
		sizes = make([]models.ThumbnailSize, 0, len(values))
		for i, item := range values {
			size, err := thumbnailSizeFromAny(item, fmt.Sprintf("%s[%d]", key, i))
			if err != nil {
				return nil, err
			}
			sizes = append(sizes, size)
		}
	default:
		return nil, invalidConfig("%s must be an array of thumbnail size objects, got %T", key, raw)
	}

	if len(sizes) == 0 {
		return nil, invalidConfig("%s must not be empty", key)
	}
	for i, size := range sizes {
		if size.Width <= 0 {
			return nil, invalidConfig("%s[%d].width must be greater than 0", key, i)
		}
		if size.Height < 0 {
			return nil, invalidConfig("%s[%d].height must be non-negative", key, i)
		}
	}
	return sizes, nil
}

func thumbnailSizeFromAny(raw any, path string) (models.ThumbnailSize, error) {
	switch value := raw.(type) {
	case models.ThumbnailSize:
		return value, nil
	case map[string]any:
		return thumbnailSizeFromMap(value, path)
	default:
		return models.ThumbnailSize{}, invalidConfig("%s must be an object, got %T", path, raw)
	}
}

func thumbnailSizeFromMap(m map[string]any, path string) (models.ThumbnailSize, error) {
	allowed := keySet("name", "width", "height", "Name", "Width", "Height")
	if err := rejectUnknownKeysAtPath(m, allowed, path); err != nil {
		return models.ThumbnailSize{}, err
	}

	var size models.ThumbnailSize
	if raw, ok := firstMapValue(m, "name", "Name"); ok {
		text, ok := raw.(string)
		if !ok {
			return models.ThumbnailSize{}, invalidConfig("%s.name must be a string, got %T", path, raw)
		}
		size.Name = text
	}
	rawWidth, ok := firstMapValue(m, "width", "Width")
	if !ok {
		return models.ThumbnailSize{}, invalidConfig("%s.width is required", path)
	}
	width, err := int64FromAny(rawWidth, path+".width")
	if err != nil {
		return models.ThumbnailSize{}, err
	}
	size.Width = int(width)

	if rawHeight, ok := firstMapValue(m, "height", "Height"); ok {
		height, err := int64FromAny(rawHeight, path+".height")
		if err != nil {
			return models.ThumbnailSize{}, err
		}
		size.Height = int(height)
	}
	return size, nil
}

func int64FromAny(raw any, key string) (int64, error) {
	switch value := raw.(type) {
	case int:
		return int64(value), nil
	case int8:
		return int64(value), nil
	case int16:
		return int64(value), nil
	case int32:
		return int64(value), nil
	case int64:
		return value, nil
	case uint:
		if uint64(value) > uint64(maxInt64Value) {
			return 0, invalidConfig("%s is outside int64 range", key)
		}
		return int64(value), nil
	case uint8:
		return int64(value), nil
	case uint16:
		return int64(value), nil
	case uint32:
		return int64(value), nil
	case uint64:
		if value > uint64(maxInt64Value) {
			return 0, invalidConfig("%s is outside int64 range", key)
		}
		return int64(value), nil
	case float64:
		return int64FromFloat(value, key)
	case float32:
		return int64FromFloat(float64(value), key)
	case json.Number:
		n, err := strconv.ParseInt(value.String(), 10, 64)
		if err != nil {
			return 0, invalidConfig("%s must be an integer: %v", key, err)
		}
		return n, nil
	default:
		return 0, invalidConfig("%s must be an integer, got %T", key, raw)
	}
}

func int64FromFloat(value float64, key string) (int64, error) {
	if math.Trunc(value) != value {
		return 0, invalidConfig("%s must be an integer, got %v", key, value)
	}
	n, err := strconv.ParseInt(strconv.FormatFloat(value, 'f', 0, 64), 10, 64)
	if err != nil {
		return 0, invalidConfig("%s is outside int64 range", key)
	}
	return n, nil
}

func rejectUnknownKeys(m map[string]any, allowed map[string]struct{}) error {
	return rejectUnknownKeysAtPath(m, allowed, "config")
}

func rejectUnknownKeysAtPath(m map[string]any, allowed map[string]struct{}, path string) error {
	for key := range m {
		if _, ok := allowed[key]; !ok {
			return invalidConfig("%s contains unknown key %q", path, key)
		}
	}
	return nil
}

func firstMapValue(m map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		value, ok := m[key]
		if ok {
			return value, true
		}
	}
	return nil, false
}

func keySet(keys ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		out[key] = struct{}{}
	}
	return out
}

func mergeKeySets(sets ...map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	for _, set := range sets {
		for key := range set {
			out[key] = struct{}{}
		}
	}
	return out
}

func invalidConfig(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, fmt.Sprintf(format, args...))
}
