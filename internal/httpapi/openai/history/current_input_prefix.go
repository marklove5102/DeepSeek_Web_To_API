package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"DeepSeek_Web_To_API/internal/auth"
	"DeepSeek_Web_To_API/internal/promptcompat"
	"DeepSeek_Web_To_API/internal/util"
)

const (
	// Tail-size targets. 32 KB target gives a healthy prefix per
	// checkpoint; 128 KB max headroom (was 64 KB) lets a long
	// conversation grow several extra turns before being forced to
	// re-anchor — observed on prod that the 64 KB cap was the dominant
	// reason long sessions kept refreshing every 3-4 turns.
	currentInputTargetTailChars = 32 * 1024
	currentInputMaxTailChars    = 128 * 1024
	currentInputPrefixTTL       = 30 * time.Minute
	currentInputPrefixMaxStates = 2048
	// Each session keeps up to N prefix variants (multi-level chain).
	// On a checkpoint refresh we PREPEND the new prefix instead of
	// overwriting, so the previous prefix stays available for the next
	// few turns until LRU evicts it. This rescues sessions that
	// occasionally compose-then-prune their tail (eg. agent flows that
	// summarise + drop earlier turns) — the older prefix still covers
	// requests that re-replay the long history.
	currentInputPrefixMaxVariants = 2
)

// currentInputPrefixMode picks how the cached prefix is delivered to upstream.
//   - prefixModeFile: prefix uploaded as a file once, file_id reused on the
//     next turn. High savings but depends on RemoteFileUpload being enabled
//     and the upload_file rate limit being acceptable. Adopted as-is from
//     cnb openclaw-tunning d8e209c.
//   - prefixModeInline: prefix bytes inlined into the user message body
//     verbatim, byte-stable across turns so upstream's prompt-prefix KV
//     cache (if any) hits, and the model sees a clear "stable context vs
//     recent turn" structural divider. No upload, no file_id — works on
//     top of our v1.0.3 default that disables RemoteFileUpload to dodge
//     upload_file rate limits.
type currentInputPrefixMode int

const (
	prefixModeFile currentInputPrefixMode = iota
	prefixModeInline
)

type currentInputPrefixState struct {
	Mode      currentInputPrefixMode
	Key       string
	Variants  []currentInputPrefixVariant // ordered most-recently-used first
	UpdatedAt time.Time
}

type currentInputPrefixVariant struct {
	PrefixText string
	PrefixHash string
	FileID     string // only meaningful for prefixModeFile
}

type currentInputPrefixPlan struct {
	Mode              currentInputPrefixMode
	Key               string
	PrefixText        string
	PrefixHash        string
	FileID            string // empty for inline mode
	TailText          string
	Reused            bool
	CheckpointRefresh bool
}

var globalCurrentInputPrefixStore = &currentInputPrefixStore{states: map[string]currentInputPrefixState{}}

type currentInputPrefixStore struct {
	mu     sync.Mutex
	states map[string]currentInputPrefixState
}

// applyCurrentInputStablePrefix is the file-upload path (prefixModeFile):
// upload the prefix once, reuse its file_id, attach as a ref. Used only
// when RemoteFileUploadEnabled() is true on the operator's config.
func (s Service) applyCurrentInputStablePrefix(ctx context.Context, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, fullText, modelType string) (promptcompat.StandardRequest, bool, error) {
	key := currentInputPrefixKeyForMode(a, stdReq, modelType, prefixModeFile)
	if key == "" {
		return stdReq, false, nil
	}

	plan, ok := globalCurrentInputPrefixStore.plan(key, fullText, prefixModeFile)
	if !ok {
		return stdReq, false, nil
	}
	if !plan.Reused {
		fileID, err := s.uploadCurrentInputFile(ctx, a, plan.PrefixText, modelType)
		if err != nil {
			return stdReq, true, err
		}
		plan.FileID = fileID
		globalCurrentInputPrefixStore.store(plan)
	}

	messages := []any{
		map[string]any{
			"role":    "user",
			"content": currentInputFilePromptWithTail(plan.TailText),
		},
	}

	stdReq.Messages = messages
	stdReq.HistoryText = fullText
	stdReq.CurrentInputFileApplied = true
	stdReq.CurrentInputPrefixHash = plan.PrefixHash
	stdReq.CurrentInputPrefixReused = plan.Reused
	stdReq.CurrentInputPrefixChars = len(plan.PrefixText)
	stdReq.CurrentInputTailChars = len(strings.TrimSpace(plan.TailText))
	stdReq.CurrentInputTailEntries = countTranscriptEntries(plan.TailText)
	stdReq.CurrentInputCheckpointRefresh = plan.CheckpointRefresh
	stdReq.RefFileIDs = prependUniqueRefFileID(stdReq.RefFileIDs, plan.FileID)
	stdReq.FinalPrompt, stdReq.ToolNames = promptcompat.BuildOpenAIPrompt(messages, stdReq.ToolsRaw, "", stdReq.ToolChoice, stdReq.Thinking)
	stdReq.RefFileTokens += util.CountPromptTokens(plan.PrefixText, stdReq.ResponseModel)
	stdReq.PromptTokenText = plan.PrefixText + "\n" + stdReq.FinalPrompt
	return stdReq, true, nil
}

// applyCurrentInputInlinePrefix is the inline path (prefixModeInline): no
// file_id, no upload. The user message body is structured as:
//
//	[stable prefix bytes — verbatim BuildOpenAICurrentInputContextTranscript output up to a transcript boundary]
//	[separator marking where the recent tail begins]
//	[recent tail bytes]
//	[trailing instruction telling the model how to read the two sections]
//
// The leading bytes (everything before the separator) are byte-identical
// across turns for the same session, courtesy of canonicalizeVolatileTranscript
// (already applied during transcript build). Upstream's prompt-prefix KV
// cache (if any) hits on those bytes; the model sees a clear "stable vs
// new" boundary that may help it focus the latest turn without
// re-deliberating the entire history.
//
// Returns (out, applied). When false, the caller falls back to the legacy
// whole-transcript inline path.
func (s Service) applyCurrentInputInlinePrefix(a *auth.RequestAuth, stdReq promptcompat.StandardRequest, fullText, modelType string) (promptcompat.StandardRequest, bool) {
	key := currentInputPrefixKeyForMode(a, stdReq, modelType, prefixModeInline)
	if key == "" {
		return stdReq, false
	}

	plan, ok := globalCurrentInputPrefixStore.plan(key, fullText, prefixModeInline)
	if !ok {
		return stdReq, false
	}
	if !plan.Reused {
		// First turn or prefix-string mismatch — record the new anchor so
		// the next turn's plan() can match against it. No upload happens.
		globalCurrentInputPrefixStore.store(plan)
	}

	body := buildInlinePrefixBody(plan.PrefixText, plan.TailText)
	messages := []any{
		map[string]any{
			"role":    "user",
			"content": body,
		},
	}

	stdReq.Messages = messages
	stdReq.HistoryText = fullText
	stdReq.CurrentInputFileApplied = true
	stdReq.CurrentInputPrefixHash = plan.PrefixHash
	stdReq.CurrentInputPrefixReused = plan.Reused
	stdReq.CurrentInputPrefixChars = len(plan.PrefixText)
	stdReq.CurrentInputTailChars = len(strings.TrimSpace(plan.TailText))
	stdReq.CurrentInputTailEntries = countTranscriptEntries(plan.TailText)
	stdReq.CurrentInputCheckpointRefresh = plan.CheckpointRefresh
	stdReq.FinalPrompt, stdReq.ToolNames = promptcompat.BuildOpenAIPrompt(messages, stdReq.ToolsRaw, "", stdReq.ToolChoice, stdReq.Thinking)
	stdReq.RefFileTokens += util.CountPromptTokens(fullText, stdReq.ResponseModel)
	stdReq.PromptTokenText = stdReq.FinalPrompt
	return stdReq, true
}

// buildInlinePrefixBody composes the user-message body for inline-prefix
// mode. Important: the FIRST byte of the body must be the first byte of
// the cached prefix, with no preface header — otherwise upstream's
// prompt-prefix KV cache (if it does naive byte-prefix matching) would
// miss every turn because the byte at position 0 differs from the
// uncached canonical-transcript form. The structural separators are
// inserted strictly AFTER the stable prefix so they cannot break leading-
// byte stability.
func buildInlinePrefixBody(prefix, tail string) string {
	prefix = strings.TrimRight(prefix, "\n")
	tail = strings.TrimSpace(tail)
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString("\n\n--- RECENT CONVERSATION TURNS ---\n\n")
	if tail != "" {
		b.WriteString(tail)
		b.WriteString("\n\n")
	}
	b.WriteString("--- INSTRUCTION ---\n")
	b.WriteString(currentInputInlinePrefixInstruction())
	return b.String()
}

func currentInputInlinePrefixInstruction() string {
	return "Everything above the \"RECENT CONVERSATION TURNS\" marker is stable prior context — treat it as background and do not re-deliberate it. The section below that marker contains the most recent turns including the latest user request. Respond to that latest user request directly."
}

// plan computes whether the new fullText can hit the cached prefix for
// `key`, and (when it cannot) returns a fresh checkpoint plan with a
// computed split. The mode parameter gates cache-hit eligibility:
// prefixModeFile additionally requires a non-empty FileID on the cached
// state, prefixModeInline only requires the prefix bytes themselves.
func (s *currentInputPrefixStore) plan(key, fullText string, mode currentInputPrefixMode) (currentInputPrefixPlan, bool) {
	fullText = strings.TrimSpace(fullText)
	if key == "" || fullText == "" {
		return currentInputPrefixPlan{}, false
	}
	fullText += "\n"
	now := time.Now()

	s.mu.Lock()
	s.pruneLocked(now)
	state, hasState := s.states[key]
	if hasState && now.Sub(state.UpdatedAt) > currentInputPrefixTTL {
		delete(s.states, key)
		hasState = false
	}
	if hasState && state.Mode == mode && len(state.Variants) > 0 {
		// Walk variants in stored order (most-recently-used first).
		// Pick the LONGEST matching prefix that still leaves a tail
		// within maxTailChars — longest-match maximises bytes the
		// upstream KV cache can re-use. File mode additionally requires
		// the variant carries a usable FileID.
		bestIdx := -1
		bestPrefixLen := 0
		for i, v := range state.Variants {
			if v.PrefixText == "" {
				continue
			}
			if mode == prefixModeFile && v.FileID == "" {
				continue
			}
			if !strings.HasPrefix(fullText, v.PrefixText) {
				continue
			}
			tailLen := len(fullText) - len(v.PrefixText)
			if tailLen > currentInputMaxTailChars {
				continue
			}
			if len(v.PrefixText) > bestPrefixLen {
				bestIdx = i
				bestPrefixLen = len(v.PrefixText)
			}
		}
		if bestIdx >= 0 {
			hot := state.Variants[bestIdx]
			tail := fullText[len(hot.PrefixText):]
			// LRU promote the hit variant to the front.
			if bestIdx != 0 {
				state.Variants = append([]currentInputPrefixVariant{hot},
					append(state.Variants[:bestIdx], state.Variants[bestIdx+1:]...)...)
			}
			state.UpdatedAt = now
			s.states[key] = state
			s.mu.Unlock()
			return currentInputPrefixPlan{
				Mode:       mode,
				Key:        key,
				PrefixText: hot.PrefixText,
				PrefixHash: hot.PrefixHash,
				FileID:     hot.FileID,
				TailText:   tail,
				Reused:     true,
			}, true
		}
	}
	s.mu.Unlock()

	prefix, tail, ok := splitCurrentInputPrefixTail(fullText)
	if !ok {
		return currentInputPrefixPlan{}, false
	}
	return currentInputPrefixPlan{
		Mode:              mode,
		Key:               key,
		PrefixText:        prefix,
		PrefixHash:        currentInputTextHash(prefix),
		TailText:          tail,
		CheckpointRefresh: true,
	}, true
}

func ActiveCurrentInputPrefixStates() int64 {
	return globalCurrentInputPrefixStore.activeStates()
}

func (s *currentInputPrefixStore) activeStates() int64 {
	if s == nil {
		return 0
	}
	now := time.Now()
	s.mu.Lock()
	s.pruneLocked(now)
	count := int64(len(s.states))
	s.mu.Unlock()
	return count
}

// store records a fresh checkpoint, prepending it to the session's
// variant chain so prior prefixes remain reusable for the next few turns.
// For file mode an empty FileID short-circuits (we cannot reuse without
// one). Inline mode tolerates empty FileID by design — the cache hit path
// checks Mode before FileID.
func (s *currentInputPrefixStore) store(plan currentInputPrefixPlan) {
	if plan.Key == "" || plan.PrefixText == "" {
		return
	}
	if plan.Mode == prefixModeFile && plan.FileID == "" {
		return
	}
	now := time.Now()
	s.mu.Lock()
	s.pruneLocked(now)
	state, ok := s.states[plan.Key]
	if !ok || state.Mode != plan.Mode {
		state = currentInputPrefixState{Mode: plan.Mode, Key: plan.Key}
	}
	// De-dup: if this prefix already lives in the chain, refresh its
	// FileID (file mode) and promote it to the head.
	for i, v := range state.Variants {
		if v.PrefixText == plan.PrefixText {
			if plan.Mode == prefixModeFile && plan.FileID != "" {
				state.Variants[i].FileID = plan.FileID
			}
			if i != 0 {
				hot := state.Variants[i]
				state.Variants = append([]currentInputPrefixVariant{hot},
					append(state.Variants[:i], state.Variants[i+1:]...)...)
			}
			state.UpdatedAt = now
			s.states[plan.Key] = state
			s.mu.Unlock()
			return
		}
	}
	// Not seen yet — prepend new variant, trim chain to bound.
	state.Variants = append([]currentInputPrefixVariant{{
		PrefixText: plan.PrefixText,
		PrefixHash: plan.PrefixHash,
		FileID:     plan.FileID,
	}}, state.Variants...)
	if len(state.Variants) > currentInputPrefixMaxVariants {
		state.Variants = state.Variants[:currentInputPrefixMaxVariants]
	}
	state.UpdatedAt = now
	s.states[plan.Key] = state
	s.mu.Unlock()
}

func (s *currentInputPrefixStore) pruneLocked(now time.Time) {
	for key, state := range s.states {
		if now.Sub(state.UpdatedAt) > currentInputPrefixTTL {
			delete(s.states, key)
		}
	}
	if len(s.states) <= currentInputPrefixMaxStates {
		return
	}
	oldestKey := ""
	oldestAt := now
	for key, state := range s.states {
		if oldestKey == "" || state.UpdatedAt.Before(oldestAt) {
			oldestKey = key
			oldestAt = state.UpdatedAt
		}
	}
	if oldestKey != "" {
		delete(s.states, oldestKey)
	}
}

// splitCurrentInputPrefixTail picks a transcript-boundary cut that
// separates "stable prefix" from "recent tail". Two modes:
//
//   - Standard mode (transcript ≥ currentInputTargetTailChars):
//     pick the cut so the tail is roughly currentInputTargetTailChars
//     bytes — large enough to carry recent context, small enough to
//     leave most of the transcript in the cacheable prefix.
//   - Soft-anchor mode (transcript < currentInputTargetTailChars):
//     short conversations would never enter the standard path, so
//     instead anchor at the LAST role block. Prefix = everything before
//     the latest role (system + earlier turns + earlier latest
//     assistant), tail = the latest role block itself. The next turn's
//     transcript is a strict superset of this prefix, so it hits the
//     cache; on every subsequent turn the prefix grows by one role
//     block, which is the natural conversation rhythm.
//
// Returns (prefix, tail, ok). ok==false means the transcript has no
// usable role-block boundary (e.g. a single message with no `=== `
// markers); the caller falls back to the legacy whole-transcript inline.
func splitCurrentInputPrefixTail(fullText string) (string, string, bool) {
	fullText = strings.TrimSpace(fullText)
	if fullText == "" {
		return "", "", false
	}
	fullText += "\n"

	// Standard mode for medium / long transcripts.
	if len(fullText) > currentInputTargetTailChars {
		desiredTailStart := len(fullText) - currentInputTargetTailChars
		cut := transcriptBoundaryAtOrAfter(fullText, desiredTailStart)
		if cut > 0 {
			prefix := fullText[:cut]
			tail := fullText[cut:]
			if len(prefix) >= currentInputTargetTailChars/4 && strings.TrimSpace(tail) != "" && len(tail) <= currentInputMaxTailChars {
				return prefix, tail, true
			}
		}
		// Fall through to soft-anchor if the standard cut couldn't find
		// a usable boundary (e.g., a single mega-message with no `=== `
		// inside the last 8 KB).
	}

	// Soft-anchor mode: cut just before the LAST role block. Even very
	// short transcripts get cached this way — first turn writes a soft
	// anchor, second turn reuses it.
	lastIdx := strings.LastIndex(fullText, "\n=== ")
	if lastIdx < 0 {
		// No role-block markers at all (transcript is malformed or a
		// single bare user message). Cannot anchor.
		return "", "", false
	}
	prefix := fullText[:lastIdx+1]
	tail := fullText[lastIdx+1:]
	if strings.TrimSpace(prefix) == "" || strings.TrimSpace(tail) == "" {
		return "", "", false
	}
	if len(tail) > currentInputMaxTailChars {
		// Last role block alone exceeds max tail — fall back to legacy
		// whole-transcript inline rather than emitting a 100 KB tail.
		return "", "", false
	}
	return prefix, tail, true
}

func transcriptBoundaryAtOrAfter(text string, start int) int {
	if start < 0 {
		start = 0
	}
	if start >= len(text) {
		return -1
	}
	idx := strings.Index(text[start:], "\n=== ")
	if idx < 0 {
		return -1
	}
	return start + idx + 1
}

func countTranscriptEntries(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	count := 0
	if strings.HasPrefix(text, "=== ") {
		count++
	}
	count += strings.Count(text, "\n=== ")
	return count
}

func currentInputFilePromptWithTail(tailText string) string {
	tailText = strings.TrimSpace(tailText)
	if tailText == "" {
		return currentInputFilePrompt()
	}
	return "Continue from the stable prefix in the attached DEEPSEEK_WEB_TO_API_HISTORY.txt context, then apply the recent conversation tail below. Answer the latest user request directly.\n\nRecent conversation tail after the attached prefix:\n" + tailText
}

// currentInputPrefixKeyForMode derives a stable cache key. The mode
// parameter matters: file mode MUST scope by upstream account because
// file_id is per-account on DeepSeek; reusing one account's file_id from
// another account fails. Inline mode uploads NOTHING and the prefix is
// just user message bytes — those are account-agnostic, so scoping by
// account would break reuse every time the empty-output retry path swaps
// accounts (observed: 30+ accounts each with their own single-hit prefix
// on prod because the 429 retry chain forced a fresh account per attempt).
// Drop the actor from the inline key so the same conversation reuses its
// prefix across all accounts that serve it.
func currentInputPrefixKeyForMode(a *auth.RequestAuth, stdReq promptcompat.StandardRequest, modelType string, mode currentInputPrefixMode) string {
	if a == nil || strings.TrimSpace(a.SessionKey) == "" {
		return ""
	}
	model := strings.TrimSpace(stdReq.ResolvedModel)
	if model == "" {
		model = strings.TrimSpace(stdReq.RequestedModel)
	}
	parts := []string{
		strings.TrimSpace(a.CallerID),
		strings.TrimSpace(a.SessionKey),
	}
	if mode == prefixModeFile {
		// File mode: MUST scope by account so file_id reuse is safe.
		actor := strings.TrimSpace(a.AccountID)
		if actor == "" && strings.TrimSpace(a.DeepSeekToken) != "" {
			actor = "direct:" + currentInputTextHash(a.DeepSeekToken)
		}
		if actor == "" {
			return ""
		}
		parts = append(parts, actor)
	} else {
		// Inline mode: no upstream upload, no account binding needed.
		// Stamp a constant marker so file-mode and inline-mode keys
		// cannot accidentally collide if a session ever flips modes.
		parts = append(parts, "inline")
	}
	parts = append(parts,
		model,
		strings.TrimSpace(modelType),
		fmt.Sprintf("thinking=%t", stdReq.Thinking),
		fmt.Sprintf("search=%t", stdReq.Search),
	)
	return strings.Join(parts, "|")
}

func currentInputTextHash(text string) string {
	if text == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}
