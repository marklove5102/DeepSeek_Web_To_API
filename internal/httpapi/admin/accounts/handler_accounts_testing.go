package accounts

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"DeepSeek_Web_To_API/internal/config"
)

type modelAliasSnapshotReader struct {
	aliases map[string]string
}

func (m modelAliasSnapshotReader) ModelAliases() map[string]string {
	return m.aliases
}

func (h *Handler) testSingleAccount(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	identifier, _ := req["identifier"].(string)
	if strings.TrimSpace(identifier) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "需要账号标识（identifier / email / mobile）"})
		return
	}
	acc, ok := findAccountByIdentifier(h.Store, identifier)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "账号不存在"})
		return
	}
	model, _ := req["model"].(string)
	if model == "" {
		model = "deepseek-v4-flash"
	}
	message, _ := req["message"].(string)
	result := h.testAccount(r.Context(), acc, model, message)
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) testAllAccounts(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	model, _ := req["model"].(string)
	if model == "" {
		model = "deepseek-v4-flash"
	}
	accounts := h.Store.Snapshot().Accounts
	if len(accounts) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"total": 0, "success": 0, "failed": 0, "results": []any{}})
		return
	}

	// Concurrent testing with a semaphore to limit parallelism.
	const maxConcurrency = 5
	results := runAccountTestsConcurrently(accounts, maxConcurrency, func(_ int, account config.Account) map[string]any {
		return h.testAccount(r.Context(), account, model, "")
	})

	success := 0
	for _, res := range results {
		if ok, _ := res["success"].(bool); ok {
			success++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": len(accounts), "success": success, "failed": len(accounts) - success, "results": results})
}

func runAccountTestsConcurrently(accounts []config.Account, maxConcurrency int, testFn func(int, config.Account) map[string]any) []map[string]any {
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	sem := make(chan struct{}, maxConcurrency)
	results := make([]map[string]any, len(accounts))
	var wg sync.WaitGroup
	for i, acc := range accounts {
		wg.Add(1)
		go func(idx int, account config.Account) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release
			results[idx] = testFn(idx, account)
		}(i, acc)
	}
	wg.Wait()
	return results
}
