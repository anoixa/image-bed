package config

import (
	"fmt"
	"strings"
	"time"
)

const defaultTimezone = "Asia/Shanghai"

// ApplyTimezone sets the process-wide local timezone used by dashboard date
// boundaries and other user-facing local time calculations.
func ApplyTimezone(name string) error {
	timezone := strings.TrimSpace(name)
	if timezone == "" {
		timezone = defaultTimezone
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return fmt.Errorf("invalid app_timezone %q: %w", timezone, err)
	}

	time.Local = loc
	return nil
}
