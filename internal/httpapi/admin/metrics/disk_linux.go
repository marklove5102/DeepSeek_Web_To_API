//go:build linux

package metrics

import "syscall"

func readDiskSnapshot(path string) diskSnapshot {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return diskSnapshot{Path: path}
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := uint64(0)
	if total >= free {
		used = total - free
	}
	return diskSnapshot{
		Path:       path,
		TotalBytes: total,
		UsedBytes:  used,
		FreeBytes:  free,
		Percent:    percent(used, total),
	}
}
