package agent

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/open-uem/wingetcfg/wingetcfg"
)

func parseTaskIDFromName(name string) int {
	prefixTrimmed := strings.TrimPrefix(name, "task_")
	parts := strings.Split(prefixTrimmed, "_")
	if len(parts) > 0 {
		if id, err := strconv.Atoi(parts[0]); err == nil {
			return id
		}
	}
	return 0
}

func parseTaskID(val any) int {
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if id := parseTaskIDFromName(v); id > 0 {
			return id
		}
		if id, err := strconv.Atoi(v); err == nil {
			return id
		}
	}
	return 0
}

func getTaskIDFromResource(r *wingetcfg.WinGetResource) int {
	if r == nil {
		return 0
	}
	if id := parseTaskIDFromName(r.ID); id > 0 {
		return id
	}
	if r.Settings != nil {
		for _, key := range []string{"ID", "id"} {
			if val, ok := r.Settings[key]; ok {
				if id := parseTaskID(val); id > 0 {
					return id
				}
			}
		}
	}
	return 0
}

func getTaskNameFromResource(r *wingetcfg.WinGetResource, taskID int) string {
	if r == nil {
		if taskID > 0 {
			return fmt.Sprintf("task_%d", taskID)
		}
		return ""
	}
	if r.ID != "" {
		return r.ID
	}
	if r.Settings != nil {
		for _, key := range []string{"ID", "id"} {
			if val, ok := r.Settings[key]; ok {
				if str, ok := val.(string); ok && str != "" {
					return str
				}
			}
		}
		for _, key := range []string{"Name", "name"} {
			if val, ok := r.Settings[key]; ok {
				if str, ok := val.(string); ok && str != "" {
					return str
				}
			}
		}
	}
	if taskID > 0 {
		return fmt.Sprintf("task_%d", taskID)
	}
	return ""
}

func getTimeoutFromSettings(settings map[string]any) time.Duration {
	if settings == nil {
		return 0
	}
	for _, key := range []string{"Timeout", "timeout"} {
		if val, ok := settings[key]; ok && val != nil {
			switch v := val.(type) {
			case int:
				if v > 0 {
					return time.Duration(v) * time.Second
				}
			case int64:
				if v > 0 {
					return time.Duration(v) * time.Second
				}
			case float64:
				if v > 0 {
					return time.Duration(v) * time.Second
				}
			case string:
				if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
					return time.Duration(sec) * time.Second
				}
			}
		}
	}
	return 0
}
