package logging

import (
	"os"
	"path/filepath"
	"time"
)

func CleanupRetention(dir string, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	entries, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil {
		return err
	}
	for _, path := range entries {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(path)
		}
	}
	return nil
}
