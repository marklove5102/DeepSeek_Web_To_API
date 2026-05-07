package responsecache

// pathPolicy tunes cache behavior per canonical request path. The zero value
// preserves historical defaults (per-caller partitioning, inherit Store-
// configured TTLs). The only knob a path can override is the caller-
// partition boundary: paths whose responses are deterministic functions of
// (path, body) — i.e. caller-agnostic — are marked SharedAcrossCallers so a
// single upstream call can serve every API key in the fleet.
//
// TTLs deliberately are NOT a per-path knob anymore. v1.0.7 briefly let a
// path override the global TTL with hardcoded values, which silently broke
// hot-reload — operators changing cache.response.{memory,disk}_ttl_seconds
// in the WebUI saw the value persist in /admin/metrics/overview but the
// LLM-business paths kept hitting the hardcoded 30 min / 48 h. v1.0.7 fix
// puts WebUI/Store TTL back as the absolute authority and bakes the v1.0.7
// optimisation defaults into Cache.defaultMemoryTTL / defaultDiskTTL so the
// hit-rate gain stays even when operators leave config at defaults.
type pathPolicy struct {
	Path                string
	SharedAcrossCallers bool
}

// pathPolicyFor returns the cache policy for a canonical request path. The
// input MUST already be passed through canonicalRequestPath(). LLM
// completions (chat / responses / messages / generateContent) deliberately
// keep per-caller partitioning: a "hit" means returning a previously-sampled
// response, and crossing the caller boundary would expose one tenant's
// replies to another. Embeddings and count_tokens are mathematically
// deterministic and do not carry that privacy boundary.
func pathPolicyFor(path string) pathPolicy {
	switch path {
	case "/v1/embeddings", "/v1/messages/count_tokens":
		return pathPolicy{Path: path, SharedAcrossCallers: true}
	}
	return pathPolicy{Path: path}
}
