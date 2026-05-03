package metrics

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type hostSnapshot struct {
	CPU       cpuSnapshot       `json:"cpu"`
	Memory    memorySnapshot    `json:"memory"`
	Disk      diskSnapshot      `json:"disk"`
	Load      loadSnapshot      `json:"load"`
	Bandwidth bandwidthSnapshot `json:"bandwidth"`
}

type cpuSnapshot struct {
	Percent float64 `json:"percent"`
	Cores   int     `json:"cores"`
}

type memorySnapshot struct {
	TotalBytes uint64  `json:"total_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	FreeBytes  uint64  `json:"free_bytes"`
	Percent    float64 `json:"percent"`
}

type diskSnapshot struct {
	Path       string  `json:"path"`
	TotalBytes uint64  `json:"total_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	FreeBytes  uint64  `json:"free_bytes"`
	Percent    float64 `json:"percent"`
}

type loadSnapshot struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
	Status string  `json:"status"`
}

type bandwidthSnapshot struct {
	RxBytesPerSecond float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSecond float64 `json:"tx_bytes_per_sec"`
	RxTotalBytes     uint64  `json:"rx_total_bytes"`
	TxTotalBytes     uint64  `json:"tx_total_bytes"`
}

type hostSampler struct {
	mu       sync.Mutex
	lastTime time.Time
	lastCPU  cpuCounters
	lastNet  netCounters
}

type cpuCounters struct {
	Idle  uint64
	Total uint64
}

type netCounters struct {
	Rx uint64
	Tx uint64
}

var defaultHostSampler = &hostSampler{}

func collectHostSnapshot(now time.Time) hostSnapshot {
	defaultHostSampler.mu.Lock()
	defer defaultHostSampler.mu.Unlock()

	cpuCounters, hasCPU := readCPUCounters()
	netCounters, hasNet := readNetCounters()

	snapshot := hostSnapshot{
		CPU:    cpuSnapshot{Cores: runtime.NumCPU()},
		Memory: readMemorySnapshot(),
		Disk:   readDiskSnapshot("/"),
		Load:   readLoadSnapshot(runtime.NumCPU()),
		Bandwidth: bandwidthSnapshot{
			RxTotalBytes: netCounters.Rx,
			TxTotalBytes: netCounters.Tx,
		},
	}

	if hasCPU && defaultHostSampler.lastCPU.Total > 0 {
		totalDelta, totalOK := deltaUint64(cpuCounters.Total, defaultHostSampler.lastCPU.Total)
		idleDelta, idleOK := deltaUint64(cpuCounters.Idle, defaultHostSampler.lastCPU.Idle)
		if totalOK && idleOK && totalDelta > 0 && idleDelta <= totalDelta {
			snapshot.CPU.Percent = round2(100 * float64(totalDelta-idleDelta) / float64(totalDelta))
		}
	}
	if hasNet && !defaultHostSampler.lastTime.IsZero() {
		elapsed := now.Sub(defaultHostSampler.lastTime).Seconds()
		if elapsed > 0 {
			if rxDelta, ok := deltaUint64(netCounters.Rx, defaultHostSampler.lastNet.Rx); ok {
				snapshot.Bandwidth.RxBytesPerSecond = round2(float64(rxDelta) / elapsed)
			}
			if txDelta, ok := deltaUint64(netCounters.Tx, defaultHostSampler.lastNet.Tx); ok {
				snapshot.Bandwidth.TxBytesPerSecond = round2(float64(txDelta) / elapsed)
			}
		}
	}

	if hasCPU {
		defaultHostSampler.lastCPU = cpuCounters
	}
	if hasNet {
		defaultHostSampler.lastNet = netCounters
	}
	defaultHostSampler.lastTime = now
	return snapshot
}

func readCPUCounters() (cpuCounters, bool) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuCounters{}, false
	}
	line := strings.SplitN(string(raw), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuCounters{}, false
	}
	var values []uint64
	for _, field := range fields[1:] {
		n, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuCounters{}, false
		}
		values = append(values, n)
	}
	var total uint64
	for _, value := range values {
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return cpuCounters{Idle: idle, Total: total}, true
}

func readMemorySnapshot() memorySnapshot {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return memorySnapshot{}
	}
	values := map[string]uint64{}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		n, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}
		values[key] = n * 1024
	}
	total := values["MemTotal"]
	free := values["MemAvailable"]
	if free == 0 {
		free = values["MemFree"]
	}
	used := uint64(0)
	if total >= free {
		used = total - free
	}
	return memorySnapshot{
		TotalBytes: total,
		UsedBytes:  used,
		FreeBytes:  free,
		Percent:    percent(used, total),
	}
}

func readLoadSnapshot(cores int) loadSnapshot {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return loadSnapshot{Status: "unknown"}
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 3 {
		return loadSnapshot{Status: "unknown"}
	}
	load1 := parseFloat(fields[0])
	load5 := parseFloat(fields[1])
	load15 := parseFloat(fields[2])
	return loadSnapshot{
		Load1:  load1,
		Load5:  load5,
		Load15: load15,
		Status: loadStatus(load1, cores),
	}
}

func readNetCounters() (netCounters, bool) {
	raw, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return netCounters{}, false
	}
	var totals netCounters
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || name == "lo" {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 16 {
			continue
		}
		rx, rxErr := strconv.ParseUint(fields[0], 10, 64)
		tx, txErr := strconv.ParseUint(fields[8], 10, 64)
		if rxErr != nil || txErr != nil {
			continue
		}
		totals.Rx += rx
		totals.Tx += tx
	}
	return totals, true
}

func loadStatus(load1 float64, cores int) string {
	if cores <= 0 || load1 <= 0 {
		return "unknown"
	}
	ratio := load1 / float64(cores)
	switch {
	case ratio >= 1:
		return "critical"
	case ratio >= 0.7:
		return "warn"
	default:
		return "ok"
	}
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return round2(100 * float64(used) / float64(total))
}

func parseFloat(value string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return round2(n)
}

func round2(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}

func deltaUint64(current, previous uint64) (uint64, bool) {
	if current < previous {
		return 0, false
	}
	return current - previous, true
}
