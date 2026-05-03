package testsuite

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPreflightStepsExactSequence(t *testing.T) {
	want := [][]string{
		{"go", "test", "./...", "-count=1"},
		{"./tests/scripts/check-node-split-syntax.sh"},
		{"node", "--test", "tests/node/stream-tool-sieve.test.js", "tests/node/js_compat_test.js"},
		{"npm", "run", "build", "--prefix", "webui"},
	}

	got := preflightSteps()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("preflight steps mismatch\nwant=%v\ngot=%v", want, got)
	}
}

func TestNewRunnerUsesAdminKeyFromConfig(t *testing.T) {
	t.Setenv("DEEPSEEK_WEB_TO_API_ADMIN_KEY", "")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"admin":{"key":"config-admin-key"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := newRunner(Options{
		ConfigPath:  configPath,
		NoPreflight: true,
		MaxKeepRuns: 1,
	})
	if err != nil {
		t.Fatalf("newRunner failed: %v", err)
	}
	if got := r.opts.AdminKey; got != "config-admin-key" {
		t.Fatalf("expected admin key from config, got %q", got)
	}
}
