package config

import (
	"testing"

	"github.com/anoixa/image-bed/storage"
	"github.com/stretchr/testify/assert"
)

func TestParseBool(t *testing.T) {
	tests := []struct {
		name         string
		val          any
		defaultValue bool
		expected     bool
	}{
		{
			name:         "bool_true",
			val:          true,
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "bool_false",
			val:          false,
			defaultValue: true,
			expected:     false,
		},
		{
			name:         "string_true",
			val:          "true",
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "string_1",
			val:          "1",
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "string_yes",
			val:          "yes",
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "string_on",
			val:          "on",
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "string_false",
			val:          "false",
			defaultValue: true,
			expected:     false,
		},
		{
			name:         "int_nonzero",
			val:          42,
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "int_zero",
			val:          0,
			defaultValue: true,
			expected:     false,
		},
		{
			name:         "int64_nonzero",
			val:          int64(100),
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "float64_nonzero",
			val:          3.14,
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "nil_value",
			val:          nil,
			defaultValue: true,
			expected:     true,
		},
		{
			name:         "unsupported_type",
			val:          []string{"test"},
			defaultValue: false,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseBool(tt.val, tt.defaultValue)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTransferModeConstants(t *testing.T) {
	// 验证 TransferMode 常量值
	assert.Equal(t, storage.TransferMode("auto"), storage.TransferModeAuto)
	assert.Equal(t, storage.TransferMode("always_proxy"), storage.TransferModeAlwaysProxy)
	assert.Equal(t, storage.TransferMode("always_direct"), storage.TransferModeAlwaysDirect)
}

func TestGlobalTransferModeKey(t *testing.T) {
	// 验证全局转发模式的 key 常量
	assert.Equal(t, "system:transfer_mode", globalTransferModeKey)
}

func TestGetStringFromMap(t *testing.T) {
	tests := []struct {
		name         string
		m            map[string]any
		key          string
		defaultValue string
		expected     string
	}{
		{
			name:         "existing_string_value",
			m:            map[string]any{"mode": "auto"},
			key:          "mode",
			defaultValue: "default",
			expected:     "auto",
		},
		{
			name:         "missing_key_returns_default",
			m:            map[string]any{"other": "value"},
			key:          "mode",
			defaultValue: "default",
			expected:     "default",
		},
		{
			name:         "nil_map_returns_default",
			m:            nil,
			key:          "mode",
			defaultValue: "default",
			expected:     "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStringFromMap(tt.m, tt.key, tt.defaultValue)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetBoolFromMap(t *testing.T) {
	tests := []struct {
		name         string
		m            map[string]any
		key          string
		defaultValue bool
		expected     bool
	}{
		{
			name:         "existing_bool_true",
			m:            map[string]any{"enabled": true},
			key:          "enabled",
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "existing_bool_false",
			m:            map[string]any{"enabled": false},
			key:          "enabled",
			defaultValue: true,
			expected:     false,
		},
		{
			name:         "string_true_value_returns_default",
			m:            map[string]any{"enabled": "true"},
			key:          "enabled",
			defaultValue: false,
			expected:     false, // getBoolFromMap 不解析字符串，只接受 bool 类型
		},
		{
			name:         "missing_key_returns_default",
			m:            map[string]any{"other": "value"},
			key:          "enabled",
			defaultValue: true,
			expected:     true,
		},
		{
			name:         "nil_map_returns_default",
			m:            nil,
			key:          "enabled",
			defaultValue: false,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getBoolFromMap(tt.m, tt.key, tt.defaultValue)
			assert.Equal(t, tt.expected, result)
		})
	}
}
