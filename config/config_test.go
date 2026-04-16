package config

import (
	"testing"

	"github.com/spf13/viper"
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

func TestBaseURLFallsBackToHostAndPort(t *testing.T) {
	cfg := &Config{
		ServerHost:   "192.168.1.10",
		ServerPort:   9000,
		ServerDomain: "",
	}

	assert.Equal(t, "http://192.168.1.10:9000", cfg.BaseURL())
}

func TestBaseURLUsesSafeDefaultsForZeroValues(t *testing.T) {
	cfg := &Config{}
	assert.Equal(t, "http://localhost:8080", cfg.BaseURL())
}

func TestBaseURLMapsWildcardHostToLocalhost(t *testing.T) {
	cfg := &Config{
		ServerHost: "0.0.0.0",
		ServerPort: 8081,
	}

	assert.Equal(t, "http://localhost:8081", cfg.BaseURL())
}

func TestSetDefaultsLeavesServerDomainEmpty(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	setDefaults()

	assert.Equal(t, "", viper.GetString("server_domain"))
}
