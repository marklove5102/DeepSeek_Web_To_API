package responses

import (
	"time"

	"DeepSeek_Web_To_API/internal/currentinputmetrics"
	"DeepSeek_Web_To_API/internal/httpapi/openai/history"
	"DeepSeek_Web_To_API/internal/promptcompat"
)

// recordCurrentInputMetrics emits one observation point describing how the
// CIF (current input file) was applied for this request: prefix-cache hit
// vs miss, the size of the tail vs the cached prefix, and the time spent
// inside applyCurrentInputFile. Lives in the responses package (mirrored
// from chat) to avoid an import cycle: history → shared → history.
func recordCurrentInputMetrics(stdReq promptcompat.StandardRequest, duration time.Duration) {
	currentinputmetrics.SetActiveStates(history.ActiveCurrentInputPrefixStates())
	currentinputmetrics.Record(currentinputmetrics.Sample{
		Applied:           stdReq.CurrentInputFileApplied,
		PrefixReused:      stdReq.CurrentInputPrefixReused,
		CheckpointRefresh: stdReq.CurrentInputCheckpointRefresh,
		PrefixHash:        stdReq.CurrentInputPrefixHash,
		PrefixChars:       stdReq.CurrentInputPrefixChars,
		TailChars:         stdReq.CurrentInputTailChars,
		TailEntries:       stdReq.CurrentInputTailEntries,
		DurationMs:        duration.Milliseconds(),
	})
}
