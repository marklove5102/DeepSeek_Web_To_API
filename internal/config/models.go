package config

import "strings"

type ModelInfo struct {
	ID         string `json:"id"`
	Object     string `json:"object"`
	Created    int64  `json:"created"`
	OwnedBy    string `json:"owned_by"`
	Permission []any  `json:"permission,omitempty"`
}

type ModelAliasReader interface {
	ModelAliases() map[string]string
}

const noThinkingModelSuffix = "-nothinking"

// deepSeekBaseModels enumerates the models the gateway accepts for inference.
// deepseek-v4-vision is intentionally absent: the upstream vision pipeline is
// disabled (see blockedDeepSeekModels), so we MUST NOT advertise it on the
// public /v1/models or /v1/models/{id} endpoints.
var deepSeekBaseModels = []ModelInfo{
	{ID: "deepseek-v4-flash", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
	{ID: "deepseek-v4-pro", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
	{ID: "deepseek-v4-flash-search", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
	{ID: "deepseek-v4-pro-search", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
}

// blockedDeepSeekModels are upstream models that exist but the operator has
// explicitly disabled. ResolveModel rejects any direct mention or alias
// mapping into this set, irrespective of the family (deepseek-v4-vision can
// be requested verbatim, via a user-defined alias map, or via legacy
// gemini-pro-vision -> v4-vision style mappings — every path is closed).
var blockedDeepSeekModels = map[string]struct{}{
	"deepseek-v4-vision": {},
}

var DeepSeekModels = appendNoThinkingVariants(deepSeekBaseModels)

var claudeBaseModels = []ModelInfo{
	// Current aliases
	{ID: "claude-opus-4-6", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-6", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-haiku-4-5", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},

	// Claude 4.x snapshots and prior aliases kept for compatibility
	{ID: "claude-sonnet-4-5", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-1", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-1-20250805", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-20250514", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-5-20250929", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-20250514", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-haiku-4-5-20251001", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},

	// Claude 3.x (legacy/deprecated snapshots and aliases)
	{ID: "claude-3-7-sonnet-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-7-sonnet-20250219", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-20240620", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-20241022", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-opus-20240229", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-sonnet-20240229", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-haiku-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-haiku-20241022", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-haiku-20240307", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
}

var ClaudeModels = appendNoThinkingVariants(claudeBaseModels)

func GetModelConfig(model string) (thinking bool, search bool, ok bool) {
	baseModel, noThinking := splitNoThinkingModel(model)
	if baseModel == "" {
		return false, false, false
	}
	if _, blocked := blockedDeepSeekModels[baseModel]; blocked {
		return false, false, false
	}
	switch baseModel {
	case "deepseek-v4-flash", "deepseek-v4-pro":
		return !noThinking, false, true
	case "deepseek-v4-flash-search", "deepseek-v4-pro-search":
		return !noThinking, true, true
	default:
		return false, false, false
	}
}

func GetModelType(model string) (modelType string, ok bool) {
	baseModel, _ := splitNoThinkingModel(model)
	if _, blocked := blockedDeepSeekModels[baseModel]; blocked {
		return "", false
	}
	switch baseModel {
	case "deepseek-v4-flash", "deepseek-v4-flash-search":
		return "default", true
	case "deepseek-v4-pro", "deepseek-v4-pro-search":
		return "expert", true
	default:
		return "", false
	}
}

func IsSupportedDeepSeekModel(model string) bool {
	_, _, ok := GetModelConfig(model)
	return ok
}

func IsNoThinkingModel(model string) bool {
	_, noThinking := splitNoThinkingModel(model)
	return noThinking
}

func DefaultModelAliases() map[string]string {
	return map[string]string{
		// OpenAI GPT / ChatGPT families
		"chatgpt-4o":          "deepseek-v4-flash",
		"gpt-4":               "deepseek-v4-flash",
		"gpt-4-turbo":         "deepseek-v4-flash",
		"gpt-4-turbo-preview": "deepseek-v4-flash",
		"gpt-4.5-preview":     "deepseek-v4-flash",
		"gpt-4o":              "deepseek-v4-flash",
		"gpt-4o-mini":         "deepseek-v4-flash",
		"gpt-4.1":             "deepseek-v4-flash",
		"gpt-4.1-mini":        "deepseek-v4-flash",
		"gpt-4.1-nano":        "deepseek-v4-flash",
		"gpt-5":               "deepseek-v4-flash",
		"gpt-5-chat":          "deepseek-v4-flash",
		"gpt-5.1":             "deepseek-v4-flash",
		"gpt-5.1-chat":        "deepseek-v4-flash",
		"gpt-5.2":             "deepseek-v4-flash",
		"gpt-5.2-chat":        "deepseek-v4-flash",
		"gpt-5.3-chat":        "deepseek-v4-flash",
		"gpt-5.4":             "deepseek-v4-flash",
		"gpt-5.5":             "deepseek-v4-flash",
		"gpt-5-mini":          "deepseek-v4-flash",
		"gpt-5-nano":          "deepseek-v4-flash",
		"gpt-5.4-mini":        "deepseek-v4-flash",
		"gpt-5.4-nano":        "deepseek-v4-flash",
		"gpt-5-pro":           "deepseek-v4-pro",
		"gpt-5.2-pro":         "deepseek-v4-pro",
		"gpt-5.4-pro":         "deepseek-v4-pro",
		"gpt-5.5-pro":         "deepseek-v4-pro",
		"gpt-5-codex":         "deepseek-v4-pro",
		"gpt-5.1-codex":       "deepseek-v4-pro",
		"gpt-5.1-codex-mini":  "deepseek-v4-pro",
		"gpt-5.1-codex-max":   "deepseek-v4-pro",
		"gpt-5.2-codex":       "deepseek-v4-pro",
		"gpt-5.3-codex":       "deepseek-v4-pro",
		"codex-mini-latest":   "deepseek-v4-pro",

		// OpenAI reasoning / research families
		"o1":                    "deepseek-v4-pro",
		"o1-preview":            "deepseek-v4-pro",
		"o1-mini":               "deepseek-v4-pro",
		"o1-pro":                "deepseek-v4-pro",
		"o3":                    "deepseek-v4-pro",
		"o3-mini":               "deepseek-v4-pro",
		"o3-pro":                "deepseek-v4-pro",
		"o3-deep-research":      "deepseek-v4-pro-search",
		"o4-mini":               "deepseek-v4-pro",
		"o4-mini-deep-research": "deepseek-v4-pro-search",

		// Claude current and historical aliases
		"claude-opus-4-6":            "deepseek-v4-pro",
		"claude-opus-4-1":            "deepseek-v4-pro",
		"claude-opus-4-1-20250805":   "deepseek-v4-pro",
		"claude-opus-4-0":            "deepseek-v4-pro",
		"claude-opus-4-20250514":     "deepseek-v4-pro",
		"claude-sonnet-4-6":          "deepseek-v4-flash",
		"claude-sonnet-4-5":          "deepseek-v4-flash",
		"claude-sonnet-4-5-20250929": "deepseek-v4-flash",
		"claude-sonnet-4-0":          "deepseek-v4-flash",
		"claude-sonnet-4-20250514":   "deepseek-v4-flash",
		"claude-haiku-4-5":           "deepseek-v4-flash",
		"claude-haiku-4-5-20251001":  "deepseek-v4-flash",
		"claude-3-7-sonnet":          "deepseek-v4-flash",
		"claude-3-7-sonnet-latest":   "deepseek-v4-flash",
		"claude-3-7-sonnet-20250219": "deepseek-v4-flash",
		"claude-3-5-sonnet":          "deepseek-v4-flash",
		"claude-3-5-sonnet-latest":   "deepseek-v4-flash",
		"claude-3-5-sonnet-20240620": "deepseek-v4-flash",
		"claude-3-5-sonnet-20241022": "deepseek-v4-flash",
		"claude-3-5-haiku":           "deepseek-v4-flash",
		"claude-3-5-haiku-latest":    "deepseek-v4-flash",
		"claude-3-5-haiku-20241022":  "deepseek-v4-flash",
		"claude-3-opus":              "deepseek-v4-pro",
		"claude-3-opus-20240229":     "deepseek-v4-pro",
		"claude-3-sonnet":            "deepseek-v4-flash",
		"claude-3-sonnet-20240229":   "deepseek-v4-flash",
		"claude-3-haiku":             "deepseek-v4-flash",
		"claude-3-haiku-20240307":    "deepseek-v4-flash",

		// Gemini current and historical text / multimodal models
		// (gemini-pro-vision intentionally not mapped — vision is disabled.
		// Requests against gemini-pro-vision will resolve to nothing and be
		// rejected at the strict allowlist gate.)
		"gemini-pro":            "deepseek-v4-pro",
		"gemini-pro-latest":     "deepseek-v4-pro",
		"gemini-flash-latest":   "deepseek-v4-flash",
		"gemini-1.5-pro":        "deepseek-v4-pro",
		"gemini-1.5-flash":      "deepseek-v4-flash",
		"gemini-1.5-flash-8b":   "deepseek-v4-flash",
		"gemini-2.0-flash":      "deepseek-v4-flash",
		"gemini-2.0-flash-lite": "deepseek-v4-flash",
		"gemini-2.5-pro":        "deepseek-v4-pro",
		"gemini-2.5-flash":      "deepseek-v4-flash",
		"gemini-2.5-flash-lite": "deepseek-v4-flash",
		"gemini-3.1-pro":        "deepseek-v4-pro",
		"gemini-3-pro":          "deepseek-v4-pro",
		"gemini-3-flash":        "deepseek-v4-flash",
		"gemini-3.1-flash":      "deepseek-v4-flash",
		"gemini-3.1-flash-lite": "deepseek-v4-flash",

		"llama-3.1-70b-instruct": "deepseek-v4-flash",
		"qwen-max":               "deepseek-v4-flash",
	}
}

// ResolveModel maps a client-requested model id to a supported DeepSeek
// model. It is strict: requests for anything outside the allowlist return
// (nil, false) — there is no heuristic fallback that maps "gpt-99-future" or
// "claude-future-pro" to a default.
//
// Allowlist rules (checked in order):
//  1. Direct hit on a supported DeepSeek model id.
//  2. Hit on the alias map (DefaultModelAliases overlaid with operator
//     model_aliases) where the target is a supported DeepSeek model id.
//  3. Strip the optional "-nothinking" suffix and retry rules 1+2; the
//     resolved model carries the suffix forward.
//
// Anything else is rejected. Operators add new mappings via WebUI
// model_aliases (hot-reloaded). Mappings whose target is an unsupported or
// blocked model id are rejected as well — operators can't accidentally
// re-enable a disabled upstream by aliasing into it.
func ResolveModel(store ModelAliasReader, requested string) (string, bool) {
	model := lower(strings.TrimSpace(requested))
	if model == "" {
		return "", false
	}
	if isBlockedModel(model) {
		return "", false
	}
	aliases := loadModelAliases(store)
	if IsSupportedDeepSeekModel(model) {
		return model, true
	}
	if mapped, ok := aliases[model]; ok && IsSupportedDeepSeekModel(mapped) && !isBlockedModel(mapped) {
		return mapped, true
	}
	baseModel, noThinking := splitNoThinkingModel(model)
	if isBlockedModel(baseModel) {
		return "", false
	}
	resolvedModel, ok := resolveCanonicalModel(aliases, baseModel)
	if !ok {
		return "", false
	}
	return withNoThinkingVariant(resolvedModel, noThinking), true
}

// isBlockedModel returns true if model (or its no-thinking base form) is on
// the blocked list. Used everywhere a string can sneak into the resolver.
func isBlockedModel(model string) bool {
	if model == "" {
		return false
	}
	if _, blocked := blockedDeepSeekModels[model]; blocked {
		return true
	}
	base, _ := splitNoThinkingModel(model)
	if _, blocked := blockedDeepSeekModels[base]; blocked {
		return true
	}
	return false
}

func isRetiredHistoricalModel(model string) bool {
	switch {
	case strings.HasPrefix(model, "claude-1."):
		return true
	case strings.HasPrefix(model, "claude-2."):
		return true
	case strings.HasPrefix(model, "claude-instant-"):
		return true
	case strings.HasPrefix(model, "gpt-3.5"):
		return true
	default:
		return false
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func OpenAIModelsResponse() map[string]any {
	return map[string]any{"object": "list", "data": DeepSeekModels}
}

func OpenAIModelByID(store ModelAliasReader, id string) (ModelInfo, bool) {
	canonical, ok := ResolveModel(store, id)
	if !ok {
		return ModelInfo{}, false
	}
	for _, model := range DeepSeekModels {
		if model.ID == canonical {
			return model, true
		}
	}
	return ModelInfo{}, false
}

func ClaudeModelsResponse() map[string]any {
	resp := map[string]any{"object": "list", "data": ClaudeModels}
	if len(ClaudeModels) > 0 {
		resp["first_id"] = ClaudeModels[0].ID
		resp["last_id"] = ClaudeModels[len(ClaudeModels)-1].ID
	} else {
		resp["first_id"] = nil
		resp["last_id"] = nil
	}
	resp["has_more"] = false
	return resp
}

func appendNoThinkingVariants(models []ModelInfo) []ModelInfo {
	out := make([]ModelInfo, 0, len(models)*2)
	for _, model := range models {
		out = append(out, model)
		variant := model
		variant.ID = withNoThinkingVariant(model.ID, true)
		out = append(out, variant)
	}
	return out
}

func splitNoThinkingModel(model string) (string, bool) {
	model = lower(strings.TrimSpace(model))
	if strings.HasSuffix(model, noThinkingModelSuffix) {
		return strings.TrimSuffix(model, noThinkingModelSuffix), true
	}
	return model, false
}

func withNoThinkingVariant(model string, enabled bool) string {
	baseModel, _ := splitNoThinkingModel(model)
	if !enabled {
		return baseModel
	}
	if baseModel == "" {
		return ""
	}
	return baseModel + noThinkingModelSuffix
}

func loadModelAliases(store ModelAliasReader) map[string]string {
	aliases := DefaultModelAliases()
	if store != nil {
		for k, v := range store.ModelAliases() {
			aliases[lower(strings.TrimSpace(k))] = lower(strings.TrimSpace(v))
		}
	}
	return aliases
}

// resolveCanonicalModel handles the post-suffix-stripped model id under the
// strict allowlist. It accepts:
//   - Direct DeepSeek model id (e.g. "deepseek-v4-pro")
//   - An alias whose target is a supported DeepSeek model id
//
// Heuristic family-prefix matching (gpt-/claude-/gemini-/o1/o3/llama-/qwen-/
// etc) was removed in v1.0.10. It silently routed any unknown id starting
// with a known family prefix to a default DeepSeek model, which made the
// model contract effectively "everything works", masked client-side typos,
// and prevented the operator from disabling specific upstream models
// (deepseek-v4-vision was reachable as long as the request id contained
// "vision"). Operators who want a custom client model id served must add
// an explicit alias in the WebUI model_aliases section.
func resolveCanonicalModel(aliases map[string]string, model string) (string, bool) {
	model = lower(strings.TrimSpace(model))
	if model == "" {
		return "", false
	}
	if isRetiredHistoricalModel(model) {
		return "", false
	}
	if IsSupportedDeepSeekModel(model) {
		return model, true
	}
	if mapped, ok := aliases[model]; ok && IsSupportedDeepSeekModel(mapped) && !isBlockedModel(mapped) {
		return mapped, true
	}
	return "", false
}
