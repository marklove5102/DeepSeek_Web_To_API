package responsecache

import "time"

// pathPolicy tunes cache behavior per canonical request path. The zero value
// preserves historical defaults (per-caller partitioning, inherit global
// TTLs). Selectively overriding for paths whose responses are deterministic
// functions of (path, body) — i.e. caller-agnostic — lets the cache share
// entries across API keys and retain them longer, which is the highest-ROI
// move identified in docs/cache-research.md.
type pathPolicy struct {
	Path                string
	SharedAcrossCallers bool
	MemoryTTL           time.Duration
	DiskTTL             time.Duration
}

const (
	// Embeddings are pure functions of (model, input, dimensions). The same
	// text always produces the same vector. Cross-caller sharing eliminates
	// redundant upstream calls; week-long disk retention is safe because
	// vectors do not drift.
	embeddingsMemoryTTL = 24 * time.Hour
	embeddingsDiskTTL   = 7 * 24 * time.Hour

	// count_tokens is deterministic given (model, messages, tools). Sharing
	// across callers is safe; medium TTLs amortize the cost of repeated
	// pre-flight token estimates from the same prompt.
	countTokensMemoryTTL = 2 * time.Hour
	countTokensDiskTTL   = 24 * time.Hour
)

// pathPolicyFor returns the cache policy for a canonical request path. The
// input MUST already be passed through canonicalRequestPath(). LLM
// completions (chat / responses / messages / generateContent) deliberately
// keep per-caller partitioning: a "hit" means returning a previously-sampled
// response, and crossing the caller boundary would expose one tenant's
// replies to another. Embeddings and count_tokens are mathematically
// deterministic and do not carry that privacy boundary.
func pathPolicyFor(path string) pathPolicy {
	switch path {
	case "/v1/embeddings":
		return pathPolicy{
			Path:                path,
			SharedAcrossCallers: true,
			MemoryTTL:           embeddingsMemoryTTL,
			DiskTTL:             embeddingsDiskTTL,
		}
	case "/v1/messages/count_tokens":
		return pathPolicy{
			Path:                path,
			SharedAcrossCallers: true,
			MemoryTTL:           countTokensMemoryTTL,
			DiskTTL:             countTokensDiskTTL,
		}
	}
	return pathPolicy{Path: path}
}
