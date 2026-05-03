//go:build !linux

package metrics

func readDiskSnapshot(path string) diskSnapshot {
	return diskSnapshot{Path: path}
}
