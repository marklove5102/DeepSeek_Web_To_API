package config

import (
	"os"
	"strconv"
	"strings"
)

func (s *Store) ModelAliases() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := DefaultModelAliases()
	for k, v := range s.cfg.ModelAliases {
		key := strings.TrimSpace(lower(k))
		val := strings.TrimSpace(lower(v))
		if key == "" || val == "" {
			continue
		}
		out[key] = val
	}
	return out
}

func (s *Store) CompatWideInputStrictOutput() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Compat.WideInputStrictOutput == nil {
		return true
	}
	return *s.cfg.Compat.WideInputStrictOutput
}

func (s *Store) CompatStripReferenceMarkers() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Compat.StripReferenceMarkers == nil {
		return true
	}
	return *s.cfg.Compat.StripReferenceMarkers
}

func (s *Store) ToolcallMode() string {
	return "feature_match"
}

func (s *Store) ToolcallEarlyEmitConfidence() string {
	return "high"
}

func (s *Store) ResponsesStoreTTLSeconds() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Responses.StoreTTLSeconds > 0 {
		return s.cfg.Responses.StoreTTLSeconds
	}
	return 900
}

func (s *Store) EmbeddingsProvider() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cfg.Embeddings.Provider)
}

func (s *Store) AutoDeleteMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mode := strings.ToLower(strings.TrimSpace(s.cfg.AutoDelete.Mode))
	switch mode {
	case "none", "single", "all":
		return mode
	}
	if s.cfg.AutoDelete.Sessions {
		return "all"
	}
	return "none"
}

// SafetyBlockMessage returns the operator-configured message returned to
// clients when a safety policy blocks a request. Empty string falls back
// to the handler default.
func (s *Store) SafetyBlockMessage() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cfg.Safety.BlockMessage)
}

func (s *Store) AdminPasswordHash() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cfg.Admin.PasswordHash)
}

func (s *Store) AdminKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cfg.Admin.Key)
}

func (s *Store) AdminJWTSecret() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cfg.Admin.JWTSecret)
}

func (s *Store) AdminJWTExpireHours() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Admin.JWTExpireHours > 0 {
		return s.cfg.Admin.JWTExpireHours
	}
	if raw := strings.TrimSpace(os.Getenv("DEEPSEEK_WEB_TO_API_JWT_EXPIRE_HOURS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 24
}

func (s *Store) AdminJWTValidAfterUnix() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Admin.JWTValidAfterUnix
}

func (s *Store) RuntimeAccountMaxInflight() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Runtime.AccountMaxInflight > 0 {
		return s.cfg.Runtime.AccountMaxInflight
	}
	if raw := strings.TrimSpace(os.Getenv("DEEPSEEK_WEB_TO_API_ACCOUNT_MAX_INFLIGHT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 2
}

func (s *Store) RuntimeAccountMaxQueue(defaultSize int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Runtime.AccountMaxQueue > 0 {
		return s.cfg.Runtime.AccountMaxQueue
	}
	if raw := strings.TrimSpace(os.Getenv("DEEPSEEK_WEB_TO_API_ACCOUNT_MAX_QUEUE")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	if defaultSize < 0 {
		return 0
	}
	return defaultSize
}

func (s *Store) RuntimeGlobalMaxInflight(defaultSize int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Runtime.GlobalMaxInflight > 0 {
		return s.cfg.Runtime.GlobalMaxInflight
	}
	if raw := strings.TrimSpace(os.Getenv("DEEPSEEK_WEB_TO_API_GLOBAL_MAX_INFLIGHT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	if defaultSize < 0 {
		return 0
	}
	return defaultSize
}

func (s *Store) RuntimeTokenRefreshIntervalHours() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Runtime.TokenRefreshIntervalHours > 0 {
		return s.cfg.Runtime.TokenRefreshIntervalHours
	}
	return 6
}

func (s *Store) AutoDeleteSessions() bool {
	return s.AutoDeleteMode() != "none"
}

func (s *Store) HistorySplitEnabled() bool {
	return false
}

func (s *Store) HistorySplitTriggerAfterTurns() int {
	return 1
}

func (s *Store) CurrentInputFileEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.CurrentInputFile.Enabled == nil {
		return true
	}
	return *s.cfg.CurrentInputFile.Enabled
}

func (s *Store) CurrentInputFileMinChars() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.CurrentInputFile.MinChars
}

func (s *Store) ThinkingInjectionEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.ThinkingInjection.Enabled == nil {
		return true
	}
	return *s.cfg.ThinkingInjection.Enabled
}

func (s *Store) ThinkingInjectionPrompt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cfg.ThinkingInjection.Prompt)
}

// RemoteFileUploadEnabled reports whether inline attachments and the
// current-input-file transcript should be forwarded to the upstream DeepSeek
// upload_file endpoint. The production default is false because that endpoint
// is per-account rate-limited and dominated the failure rate on busy
// workloads. Operators can opt in via the
// DEEPSEEK_WEB_TO_API_REMOTE_FILE_UPLOAD_ENABLED env var when they have
// headroom; the JSON config field server.remote_file_upload_enabled is also
// honoured for parity with other knobs.
func (s *Store) RemoteFileUploadEnabled() bool {
	if raw := strings.TrimSpace(os.Getenv("DEEPSEEK_WEB_TO_API_REMOTE_FILE_UPLOAD_ENABLED")); raw != "" {
		return parseBoolDefault(raw, false)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Server.RemoteFileUploadEnabled != nil {
		return *s.cfg.Server.RemoteFileUploadEnabled
	}
	return false
}
