package config

import "errors"

var (
	// ErrCannotDisableDefaultStorage 表示不能禁用当前默认存储（须先切换默认）。
	ErrCannotDisableDefaultStorage = errors.New("cannot disable the default storage configuration")
	// ErrCannotSetDisabledStorageDefault 表示不能把禁用的存储设为默认。
	ErrCannotSetDisabledStorageDefault = errors.New("cannot set a disabled storage configuration as default")
	// ErrStorageDefaultChangeRequiresEndpoint 表示存储默认项只能通过专用端点切换。
	ErrStorageDefaultChangeRequiresEndpoint = errors.New("storage default can only be changed via the default endpoint")
	// ErrConfigCategoryMismatch 表示更新请求不能改变已有配置的类别。
	ErrConfigCategoryMismatch = errors.New("configuration category does not match the stored configuration")
)
