package utils

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestTextLogHandlerUsesMessagePrefixModule(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newTextLogHandler(&buf, slog.LevelDebug))

	logger.Info("[Worker] started")

	output := buf.String()
	if !strings.Contains(output, "[Worker][INFO] started") {
		t.Fatalf("expected worker-prefixed output, got %q", output)
	}
}

func TestTextLogHandlerUsesModuleAttr(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newTextLogHandler(&buf, slog.LevelDebug))

	logger.LogAttrs(context.Background(), slog.LevelWarn, "queue full", slog.String(logModuleAttrKey, "Pipeline"))

	output := buf.String()
	if !strings.Contains(output, "[Pipeline][WARN] queue full") {
		t.Fatalf("expected module attr output, got %q", output)
	}
}

func TestTextLogHandlerDefaultsModule(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newTextLogHandler(&buf, slog.LevelDebug))

	logger.Error("boom")

	output := buf.String()
	if !strings.Contains(output, "[App][ERROR] boom") {
		t.Fatalf("expected default module output, got %q", output)
	}
}

func TestParseModulePrefixSkipsLegacyDebugPrefix(t *testing.T) {
	module, message := parseModulePrefix("[DEBUG][GetDailyStats] querying")
	if module != "GetDailyStats" {
		t.Fatalf("expected module GetDailyStats, got %q", module)
	}
	if message != "querying" {
		t.Fatalf("expected stripped message, got %q", message)
	}
}
