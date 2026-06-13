package config

import "errors"

var (
	// ErrCannotDisableDefaultStorage 表示不能禁用当前默认存储（须先切换默认）。
	ErrCannotDisableDefaultStorage = errors.New("cannot disable the default storage configuration")
	// ErrCannotSetDisabledStorageDefault 表示不能把禁用的存储设为默认。
	ErrCannotSetDisabledStorageDefault = errors.New("cannot set a disabled storage configuration as default")
)
