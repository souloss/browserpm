package browserpm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"runtime"

	"github.com/shirou/gopsutil/v3/process"
)

// generateID 生成随机 ID
func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// getProcessMemory 获取进程内存信息
func getProcessMemory(ctx context.Context, pid int32) (rss, vms, exclusive uint64) {
	p, err := process.NewProcess(pid)
	if err != nil {
		return 0, 0, 0
	}

	// 基础内存信息（所有平台可用）
	if mi, err := p.MemoryInfo(); err == nil {
		rss = mi.RSS
		vms = mi.VMS
		exclusive = rss // 默认退化策略
	}

	// Linux：优先使用 PSS（smaps_rollup）
	// MemoryMapsWithContext(ctx, true) 在 Linux >= 4.15 会直接读 smaps_rollup
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
