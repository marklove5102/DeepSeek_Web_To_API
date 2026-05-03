package config

import (
	"testing"
	"time"
)

func TestHTTPTotalTimeout(t *testing.T) {
	t.Setenv("DEEPSEEK_WEB_TO_API_HTTP_TOTAL_TIMEOUT_SECONDS", "")
	if got := HTTPTotalTimeout(); got != 7200*time.Second {
		t.Fatalf("expected default 7200s, got %s", got)
	}

	t.Setenv("DEEPSEEK_WEB_TO_API_HTTP_TOTAL_TIMEOUT_SECONDS", "1800")
	if got := HTTPTotalTimeout(); got != 1800*time.Second {
		t.Fatalf("expected configured 1800s, got %s", got)
	}

	t.Setenv("DEEPSEEK_WEB_TO_API_HTTP_TOTAL_TIMEOUT_SECONDS", "invalid")
	if got := HTTPTotalTimeout(); got != 7200*time.Second {
		t.Fatalf("expected invalid value to fall back to 7200s, got %s", got)
	}
}
