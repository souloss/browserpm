package browserpm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"runtime"

	"github.com/shirou/gopsutil/v3/process"
)

// getEnvOrDefault gets the environment variable or returns the default value.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// generateID generates a random ID.
func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// getProcessMemory gets the process memory information.
func getProcessMemory(ctx context.Context, pid int32) (rss, vms, exclusive uint64) {
	p, err := process.NewProcess(pid)
	if err != nil {
		return 0, 0, 0
	}

	// Basic memory information (available on all platforms).
	if mi, err := p.MemoryInfo(); err == nil {
		rss = mi.RSS
		vms = mi.VMS
		exclusive = rss // default fallback strategy
	}

	// Linux: use PSS (smaps_rollup) if available.
	// MemoryMapsWithContext(ctx, true) will read smaps_rollup directly on Linux >= 4.15.
	if runtime.GOOS == "linux" {
		if maps, err := p.MemoryMapsWithContext(ctx, true); err == nil {
			if maps != nil && len(*maps) > 0 {
				if (*maps)[0].Pss > 0 {
					// KB to Bytes
					exclusive = (*maps)[0].Pss * 1024
				}
			}
		}
	}

	return rss, vms, exclusive
}
