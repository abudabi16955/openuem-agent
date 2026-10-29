package agent

import (
	"testing"
	"time"

	"github.com/open-uem/wingetcfg/wingetcfg"
)

func TestParseTaskIDFromName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"standard task prefix", "task_15_1", 15},
		{"task prefix without version", "task_99", 99},
		{"numeric string", "123", 123},
		{"empty string", "", 0},
		{"non numeric", "custom_task", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTaskIDFromName(tt.in)
			if got != tt.want {
				t.Errorf("parseTaskIDFromName(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseTaskID(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want int
	}{
		{"int", 42, 42},
		{"int64", int64(42), 42},
		{"float64", float64(42), 42},
		{"string task prefix", "task_42_2", 42},
		{"string number", "42", 42},
		{"nil", nil, 0},
		{"invalid string", "not_a_number", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTaskID(tt.in)
			if got != tt.want {
				t.Errorf("parseTaskID(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestGetTaskIDFromResource(t *testing.T) {
	tests := []struct {
		name string
		res  *wingetcfg.WinGetResource
		want int
	}{
		{
			name: "from resource ID",
			res:  &wingetcfg.WinGetResource{ID: "task_10_1"},
			want: 10,
		},
		{
			name: "from settings ID string",
			res: &wingetcfg.WinGetResource{
				Settings: map[string]any{"ID": "task_20_1"},
			},
			want: 20,
		},
		{
			name: "from settings id int",
			res: &wingetcfg.WinGetResource{
				Settings: map[string]any{"id": 30},
			},
			want: 30,
		},
		{
			name: "from settings id float64",
			res: &wingetcfg.WinGetResource{
				Settings: map[string]any{"id": float64(35)},
			},
			want: 35,
		},
		{
			name: "nil resource",
			res:  nil,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getTaskIDFromResource(tt.res)
			if got != tt.want {
				t.Errorf("getTaskIDFromResource() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetTaskNameFromResource(t *testing.T) {
	r1 := &wingetcfg.WinGetResource{ID: "explicit_name"}
	if got := getTaskNameFromResource(r1, 10); got != "explicit_name" {
		t.Errorf("got %q, want explicit_name", got)
	}

	r2 := &wingetcfg.WinGetResource{
		Settings: map[string]any{"ID": "task_20_1"},
	}
	if got := getTaskNameFromResource(r2, 20); got != "task_20_1" {
		t.Errorf("got %q, want task_20_1", got)
	}

	r3 := &wingetcfg.WinGetResource{}
	if got := getTaskNameFromResource(r3, 30); got != "task_30" {
		t.Errorf("got %q, want task_30", got)
	}
}

func TestGetTimeoutFromSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		want     time.Duration
	}{
		{
			name:     "int Timeout",
			settings: map[string]any{"Timeout": 60},
			want:     60 * time.Second,
		},
		{
			name:     "string timeout",
			settings: map[string]any{"timeout": "120"},
			want:     120 * time.Second,
		},
		{
			name:     "float64 Timeout",
			settings: map[string]any{"Timeout": float64(90)},
			want:     90 * time.Second,
		},
		{
			name:     "nil settings",
			settings: nil,
			want:     0,
		},
		{
			name:     "negative timeout",
			settings: map[string]any{"Timeout": -10},
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getTimeoutFromSettings(tt.settings)
			if got != tt.want {
				t.Errorf("getTimeoutFromSettings() = %v, want %v", got, tt.want)
			}
		})
	}
}
