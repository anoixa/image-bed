package format

import (
	"fmt"
)

const (
	byteUnit = 1024
)

var units = []string{"B", "KB", "MB", "GB", "TB", "PB"}

// HumanReadableSize 将字节数转换为人类可读的格式
func HumanReadableSize(bytes int64) string {
	if bytes < byteUnit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(byteUnit), 1
	for n := bytes / byteUnit; n >= byteUnit && exp < len(units)-1; n /= byteUnit {
		div *= byteUnit
		exp++
	}

	return fmt.Sprintf("%.2f %s", float64(bytes)/float64(div), units[exp])
}

// HumanReadableSizeWithPrecision 自定义精度转换
func HumanReadableSizeWithPrecision(bytes int64, precision int) string {
	if bytes < byteUnit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(byteUnit), 1
	for n := bytes / byteUnit; n >= byteUnit && exp < len(units)-1; n /= byteUnit {
		div *= byteUnit
		exp++
	}

	format := fmt.Sprintf("%%.%df %%s", precision)
	return fmt.Sprintf(format, float64(bytes)/float64(div), units[exp])
}
