package selfservice

import (
	"strconv"
	"strings"
	"unicode"
)

// CompareVersions compares two version strings (v1 and v2).
// Returns:
//   -1 if v1 < v2
//    0 if v1 == v2
//    1 if v1 > v2
// Handles semver and common version patterns (e.g. "1.2.3", "v2.0.1", "1.10", "2.0-beta").
func CompareVersions(v1, v2 string) int {
	clean1 := cleanVersionString(v1)
	clean2 := cleanVersionString(v2)

	if clean1 == clean2 {
		return 0
	}
	if clean1 == "" {
		return -1
	}
	if clean2 == "" {
		return 1
	}

	parts1 := splitVersionParts(clean1)
	parts2 := splitVersionParts(clean2)

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		var p1, p2 string
		if i < len(parts1) {
			p1 = parts1[i]
		}
		if i < len(parts2) {
			p2 = parts2[i]
		}

		num1, isNum1 := parseNumber(p1)
		num2, isNum2 := parseNumber(p2)

		if isNum1 && isNum2 {
			if num1 < num2 {
				return -1
			}
			if num1 > num2 {
				return 1
			}
			continue
		}

		if isNum1 && !isNum2 {
			// Numbers are considered newer than missing or prerelease text
			if p2 == "" {
				if num1 > 0 {
					return 1
				}
				continue
			}
			return 1
		}
		if !isNum1 && isNum2 {
			if p1 == "" {
				if num2 > 0 {
					return -1
				}
				continue
			}
			return -1
		}

		// Both are string components
		cmp := strings.Compare(strings.ToLower(p1), strings.ToLower(p2))
		if cmp != 0 {
			return cmp
		}
	}

	return 0
}

func cleanVersionString(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

func splitVersionParts(v string) []string {
	// Split by '.', '-', '_', or '+'
	return strings.FieldsFunc(v, func(r rune) bool {
		return r == '.' || r == '-' || r == '_' || r == '+'
	})
}

func parseNumber(s string) (uint64, bool) {
	if s == "" {
		return 0, true
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
