package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCorsOriginsTrimsAndDropsEmptyEntries(t *testing.T) {
	cfg := &Config{
		CorsOrigins: " http://localhost:5173 , , http://127.0.0.1:5173  ",
	}

	assert.Equal(t, []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
	}, cfg.GetCorsOrigins())
}
