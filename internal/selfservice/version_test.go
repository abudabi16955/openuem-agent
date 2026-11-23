package selfservice

import (
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"1.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.10.0", "1.2.0", 1},
		{"1.2.0", "1.10.0", -1},
		{"2.0", "1.9.9", 1},
		{"2.0.0", "2.0.1", -1},
		{"", "1.0", -1},
		{"1.0", "", 1},
		{"", "", 0},
		{"v2.1.3", "v2.1.2", 1},
		{"1.0.0-beta", "1.0.0", -1},
	}

	for _, tc := range tests {
		t.Run(tc.v1+"_vs_"+tc.v2, func(t *testing.T) {
			res := CompareVersions(tc.v1, tc.v2)
			if (tc.expected > 0 && res <= 0) || (tc.expected < 0 && res >= 0) || (tc.expected == 0 && res != 0) {
				t.Errorf("CompareVersions(%q, %q) = %d; expected sign of %d", tc.v1, tc.v2, res, tc.expected)
			}
		})
	}
}
