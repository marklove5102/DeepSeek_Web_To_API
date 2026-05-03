package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultHTTPTotalTimeoutSeconds = 7200

func HTTPTotalTimeout() time.Duration {
	return envDurationSeconds("DEEPSEEK_WEB_TO_API_HTTP_TOTAL_TIMEOUT_SECONDS", defaultHTTPTotalTimeoutSeconds)
}

func envDurationSeconds(name string, defaultSeconds int) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return time.Duration(defaultSeconds) * time.Second
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return time.Duration(defaultSeconds) * time.Second
	}
	return time.Duration(seconds) * time.Second
}
