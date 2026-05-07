package currentinputmetrics

import (
	"math"
	"sort"
	"sync"
)

const maxTailSamples = 4096

type Sample struct {
	Applied           bool
	PrefixReused      bool
	CheckpointRefresh bool
	PrefixHash        string
	PrefixChars       int
	TailChars         int
	TailEntries       int
	DurationMs        int64
}

type Snapshot struct {
	TotalSeen                    int64   `json:"total_seen"`   // every chat completion that traversed the apply step
	Applied                      int64   `json:"applied"`      // subset where CIF actually rewrote the request
	TriggerRate                  float64 `json:"trigger_rate"` // applied / total_seen × 100 — % of requests that benefited
	Reused                       int64   `json:"reused"`
	Refreshes                    int64   `json:"refreshes"`
	FallbackFullUploads          int64   `json:"fallback_full_uploads"`
	ReuseRate                    float64 `json:"reuse_rate"` // reused / applied × 100 — within CIF-touched requests
	ActiveStates                 int64   `json:"active_states"`
	TailCharsAvg                 int64   `json:"tail_chars_avg"`
	TailCharsP95                 int64   `json:"tail_chars_p95"`
	TailEntriesAvg               float64 `json:"tail_entries_avg"`
	PrefixCharsAvg               int64   `json:"prefix_chars_avg"`
	CurrentInputFileMsAvg        int64   `json:"current_input_file_ms_avg"`
	CurrentInputFileMsReusedAvg  int64   `json:"current_input_file_ms_reused_avg"`
	CurrentInputFileMsRefreshAvg int64   `json:"current_input_file_ms_refresh_avg"`
	LastPrefixHash               string  `json:"last_prefix_hash,omitempty"`
	LastReused                   bool    `json:"last_reused"`
	LastTailChars                int     `json:"last_tail_chars"`
}

type recorder struct {
	mu sync.Mutex

	totalSeen           int64
	applied             int64
	reused              int64
	refreshes           int64
	fallbackFullUploads int64

	tailCharsTotal       int64
	tailEntriesTotal     int64
	prefixCharsTotal     int64
	durationTotalMs      int64
	reusedDurationMs     int64
	refreshDurationMs    int64
	reusedDurationCount  int64
	refreshDurationCount int64

	tailSamples []int

	lastPrefixHash string
	lastReused     bool
	lastTailChars  int
	activeStates   int64
}

var global recorder

// Record observes one request that traversed the CIF apply step.
// Sample.Applied=false marks a request that was eligible (the apply step
// ran) but where CIF chose not to rewrite the prompt — for example because
// the latest user message was below threshold or the transcript had no
// usable role boundaries. Both cases bump TotalSeen so trigger_rate is
// meaningful (otherwise we'd only count the requests where CIF *did*
// fire and trigger_rate would always be 100%).
func Record(sample Sample) {
	global.mu.Lock()
	defer global.mu.Unlock()

	global.totalSeen++
	if !sample.Applied {
		// Even when CIF was a no-op we still want the lastTailChars /
		// lastReused fields to reflect "no apply" so the WebUI can show
		// the most-recent decision distinctly.
		global.lastReused = false
		return
	}

	global.applied++
	if sample.PrefixReused {
		global.reused++
		global.reusedDurationMs += sample.DurationMs
		global.reusedDurationCount++
	}
	if sample.CheckpointRefresh {
		global.refreshes++
		global.refreshDurationMs += sample.DurationMs
		global.refreshDurationCount++
		if sample.TailChars <= 0 {
			global.fallbackFullUploads++
		}
	}
	global.tailCharsTotal += int64(sample.TailChars)
	global.tailEntriesTotal += int64(sample.TailEntries)
	global.prefixCharsTotal += int64(sample.PrefixChars)
	global.durationTotalMs += sample.DurationMs
	global.tailSamples = append(global.tailSamples, sample.TailChars)
	if len(global.tailSamples) > maxTailSamples {
		copy(global.tailSamples, global.tailSamples[len(global.tailSamples)-maxTailSamples:])
		global.tailSamples = global.tailSamples[:maxTailSamples]
	}
	global.lastPrefixHash = sample.PrefixHash
	global.lastReused = sample.PrefixReused
	global.lastTailChars = sample.TailChars
}

func SetActiveStates(active int64) {
	global.mu.Lock()
	if active < 0 {
		active = 0
	}
	global.activeStates = active
	global.mu.Unlock()
}

func GetSnapshot() Snapshot {
	global.mu.Lock()
	defer global.mu.Unlock()
	return global.snapshotLocked()
}

func ResetForTest() {
	global.mu.Lock()
	global.totalSeen = 0
	global.applied = 0
	global.reused = 0
	global.refreshes = 0
	global.fallbackFullUploads = 0
	global.tailCharsTotal = 0
	global.tailEntriesTotal = 0
	global.prefixCharsTotal = 0
	global.durationTotalMs = 0
	global.reusedDurationMs = 0
	global.refreshDurationMs = 0
	global.reusedDurationCount = 0
	global.refreshDurationCount = 0
	global.tailSamples = nil
	global.lastPrefixHash = ""
	global.lastReused = false
	global.lastTailChars = 0
	global.activeStates = 0
	global.mu.Unlock()
}

func (r *recorder) snapshotLocked() Snapshot {
	out := Snapshot{
		TotalSeen:           r.totalSeen,
		Applied:             r.applied,
		Reused:              r.reused,
		Refreshes:           r.refreshes,
		FallbackFullUploads: r.fallbackFullUploads,
		ActiveStates:        r.activeStates,
		LastPrefixHash:      r.lastPrefixHash,
		LastReused:          r.lastReused,
		LastTailChars:       r.lastTailChars,
	}
	if r.totalSeen > 0 {
		out.TriggerRate = round2(float64(r.applied) * 100 / float64(r.totalSeen))
	}
	if r.applied > 0 {
		out.ReuseRate = round2(float64(r.reused) * 100 / float64(r.applied))
		out.TailCharsAvg = r.tailCharsTotal / r.applied
		out.PrefixCharsAvg = r.prefixCharsTotal / r.applied
		out.CurrentInputFileMsAvg = r.durationTotalMs / r.applied
		out.TailEntriesAvg = round2(float64(r.tailEntriesTotal) / float64(r.applied))
	}
	if r.reusedDurationCount > 0 {
		out.CurrentInputFileMsReusedAvg = r.reusedDurationMs / r.reusedDurationCount
	}
	if r.refreshDurationCount > 0 {
		out.CurrentInputFileMsRefreshAvg = r.refreshDurationMs / r.refreshDurationCount
	}
	out.TailCharsP95 = percentileInt(r.tailSamples, 0.95)
	return out
}

func percentileInt(samples []int, q float64) int64 {
	if len(samples) == 0 {
		return 0
	}
	cp := append([]int(nil), samples...)
	sort.Ints(cp)
	if q <= 0 {
		return int64(cp[0])
	}
	if q >= 1 {
		return int64(cp[len(cp)-1])
	}
	idx := int(math.Ceil(float64(len(cp))*q)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return int64(cp[idx])
}

func round2(v float64) float64 {
	if v == 0 {
		return 0
	}
	if v > 0 {
		return float64(int(v*100+0.5)) / 100
	}
	return float64(int(v*100-0.5)) / 100
}
