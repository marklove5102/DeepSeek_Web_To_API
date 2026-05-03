package config

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

func loadStore() (*Store, error) {
	cfg, fromEnv, err := loadConfig()
	cfg.NormalizeCredentials()
	if validateErr := ValidateConfig(cfg); validateErr != nil {
		err = errors.Join(err, validateErr)
	}
	return &Store{cfg: cfg, path: ConfigPath(), fromEnv: fromEnv}, err
}

func loadConfig() (Config, bool, error) {
	rawCfg := strings.TrimSpace(os.Getenv("DEEPSEEK_WEB_TO_API_CONFIG_JSON"))
	if rawCfg != "" {
		return loadConfigFromEnv(rawCfg)
	}
	return loadConfigFromPrimaryFile()
}

func loadConfigFromEnv(rawCfg string) (Config, bool, error) {
	cfg, err := parseConfigString(rawCfg)
	if err != nil {
		if envWritebackEnabled() {
			if fileCfg, fileErr := loadConfigFromFile(ConfigPath()); fileErr == nil {
				return fileCfg, false, nil
			}
		}
		return cfg, true, err
	}
	cfg.ClearAccountTokens()
	cfg.DropInvalidAccounts()
	if !envWritebackEnabled() {
		return cfg, true, err
	}
	return loadOrBootstrapEnvWritebackConfig(cfg, err)
}

func loadOrBootstrapEnvWritebackConfig(cfg Config, parseErr error) (Config, bool, error) {
	// #nosec G304 -- ConfigPath is an operator-controlled local config path.
	content, fileErr := os.ReadFile(ConfigPath())
	if fileErr == nil {
		var fileCfg Config
		if unmarshalErr := json.Unmarshal(content, &fileCfg); unmarshalErr == nil {
			fileCfg.DropInvalidAccounts()
			return fileCfg, false, parseErr
		}
	}
	if errors.Is(fileErr, os.ErrNotExist) {
		if validateErr := ValidateConfig(cfg); validateErr != nil {
			return cfg, true, validateErr
		}
		if writeErr := writeConfigFile(ConfigPath(), cfg.Clone()); writeErr == nil {
			return cfg, false, parseErr
		} else {
			Logger.Warn("[config] env writeback bootstrap failed", "error", writeErr)
		}
	}
	return cfg, true, parseErr
}

func loadConfigFromPrimaryFile() (Config, bool, error) {
	cfg, err := loadConfigFromFile(ConfigPath())
	if err != nil {
		if legacyCfg, ok := loadLegacyContainerConfigIfNeeded(); ok {
			return legacyCfg, false, nil
		}
		return Config{}, false, err
	}
	return cfg, false, nil
}

func loadLegacyContainerConfigIfNeeded() (Config, bool) {
	if !shouldTryLegacyContainerConfigPath() {
		return Config{}, false
	}
	legacyPath := legacyContainerConfigPath()
	legacyCfg, legacyErr := loadConfigFromFile(legacyPath)
	if legacyErr != nil {
		return Config{}, false
	}
	Logger.Info("[config] loaded legacy container config path", "path", legacyPath)
	return legacyCfg, true
}

func loadConfigFromFile(path string) (Config, error) {
	// #nosec G304 -- config loading reads an operator-controlled local config path.
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(content, &cfg); err != nil {
		return Config{}, err
	}
	cfg.NormalizeCredentials()
	cfg.DropInvalidAccounts()
	if strings.Contains(string(content), `"test_status"`) {
		if b, err := json.MarshalIndent(cfg, "", "  "); err == nil {
			_ = os.WriteFile(path, b, 0o600)
		}
	}
	return cfg, nil
}
