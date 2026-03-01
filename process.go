package browserpm

import "context"

// GetProcessInfos retrieves resource usage (CPU, RSS, VMS, PSS) for all
// Chromium sub-processes via the CDP SystemInfo.getProcessInfo command.
func (m *BrowserManager) GetProcessInfos(ctx context.Context) ([]ProcessInfo, error) {
	m.mu.Lock()
	cdp := m.cdpSession
	m.mu.Unlock()

	if cdp == nil {
		return nil, NewError(ErrInvalidState, "CDP session is nil")
	}

	resp, err := cdp.Send("SystemInfo.getProcessInfo", map[string]interface{}{})
	if err != nil {
		return nil, WrapError(err, ErrInternal, "failed to send CDP command")
	}

	raw, ok := resp.(map[string]interface{})
	if !ok {
		return nil, NewError(ErrInternal, "invalid CDP response format")
	}

	rawList, ok := raw["processInfo"].([]interface{})
	if !ok {
		return nil, NewError(ErrInternal, "processInfo field missing or invalid")
	}

	infos := make([]ProcessInfo, 0, len(rawList))
	for _, item := range rawList {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		pidFloat, ok := entry["id"].(float64)
		if !ok {
			continue
		}
		pid := int32(pidFloat)

		pi := ProcessInfo{
			ID:  pid,
			CPU: safeFloat(entry["cpuTime"]),
		}
		if t, ok := entry["type"].(string); ok {
			pi.Type = t
		}

		rss, vms, exclusive := getProcessMemory(ctx, pid)
		pi.RSS = rss
		pi.VMS = vms
		pi.ExclusiveMemory = exclusive

		infos = append(infos, pi)
	}

	return infos, nil
}

func safeFloat(v interface{}) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}
