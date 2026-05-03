package webui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"DeepSeek_Web_To_API/internal/config"
)

const (
	defaultBuildTimeout = 5 * time.Minute
)

func EnsureBuiltOnStartup(store *config.Store) {
	if !shouldAutoBuild(store) {
		return
	}
	staticDir := resolveStaticAdminDir(config.StaticAdminDir())
	if store != nil {
		staticDir = resolveStaticAdminDir(store.StaticAdminDir())
	}
	if hasBuiltUI(staticDir) {
		return
	}
	if err := buildWebUI(staticDir); err != nil {
		config.Logger.Warn("[webui] auto build failed", "error", err)
		return
	}
	if hasBuiltUI(staticDir) {
		config.Logger.Info("[webui] auto build completed", "dir", staticDir)
		return
	}
	config.Logger.Warn("[webui] auto build finished but output missing", "dir", staticDir)
}

func shouldAutoBuild(store *config.Store) bool {
	if store != nil {
		return store.ServerAutoBuildWebUI()
	}
	raw := strings.TrimSpace(os.Getenv("DEEPSEEK_WEB_TO_API_AUTO_BUILD_WEBUI"))
	if raw == "" {
		return true
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func hasBuiltUI(staticDir string) bool {
	if strings.TrimSpace(staticDir) == "" {
		return false
	}
	indexPath := filepath.Join(staticDir, "index.html")
	st, err := os.Stat(indexPath)
	return err == nil && !st.IsDir()
}

func buildWebUI(staticDir string) error {
	if _, err := exec.LookPath("npm"); err != nil {
		return fmt.Errorf("npm not found in PATH: %w", err)
	}
	if strings.TrimSpace(staticDir) == "" {
		return errors.New("static admin dir is empty")
	}
	cleanStaticDir, err := filepath.Abs(filepath.Clean(staticDir))
	if err != nil {
		return err
	}

	config.Logger.Info("[webui] static files missing, running npm build")
	ctx, cancel := context.WithTimeout(context.Background(), defaultBuildTimeout)
	defer cancel()

	if _, err := os.Stat(filepath.Join("webui", "node_modules")); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		installCmd := exec.CommandContext(ctx, "npm", "ci", "--prefix", "webui")
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("webui npm ci timed out after %s", defaultBuildTimeout)
			}
			return err
		}
	}

	if err := os.MkdirAll(cleanStaticDir, 0o750); err != nil {
		return err
	}
	// #nosec G204 - command and flags are fixed; cleanStaticDir is passed as a single argv value without shell expansion.
	cmd := exec.CommandContext(ctx, "npm", "run", "build", "--prefix", "webui", "--", "--outDir", cleanStaticDir, "--emptyOutDir")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("webui build timed out after %s", defaultBuildTimeout)
		}
		return err
	}
	return nil
}
