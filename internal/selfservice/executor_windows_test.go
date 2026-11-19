//go:build windows

package selfservice

import (
	"reflect"
	"testing"
	"unicode/utf16"
)

func createUTF16Block(vars []string) []uint16 {
	var block []uint16
	for _, v := range vars {
		block = append(block, utf16.Encode([]rune(v))...)
		block = append(block, 0) // null-terminator for string
	}
	block = append(block, 0) // double null-terminator for block
	return block
}

func TestParseEnvBlock(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
		isNil    bool
	}{
		{
			name:     "nil block",
			isNil:    true,
			expected: nil,
		},
		{
			name:     "empty block",
			input:    []string{},
			expected: nil,
		},
		{
			name:     "single variable",
			input:    []string{"VAR1=VALUE1"},
			expected: []string{"VAR1=VALUE1"},
		},
		{
			name: "multiple variables",
			input: []string{
				"ALLUSERSPROFILE=C:\\ProgramData",
				"APPDATA=C:\\Users\\User\\AppData\\Roaming",
				"HOMEPATH=\\Users\\User",
				"PATH=C:\\Windows;C:\\Windows\\System32",
				"USERNAME=User",
			},
			expected: []string{
				"ALLUSERSPROFILE=C:\\ProgramData",
				"APPDATA=C:\\Users\\User\\AppData\\Roaming",
				"HOMEPATH=\\Users\\User",
				"PATH=C:\\Windows;C:\\Windows\\System32",
				"USERNAME=User",
			},
		},
		{
			name: "variable with multiple equals signs and unicode",
			input: []string{
				"COMPLEX=a=b=c=d",
				"UNICODE_VAL=Zażółć gęślą jaźń",
			},
			expected: []string{
				"COMPLEX=a=b=c=d",
				"UNICODE_VAL=Zażółć gęślą jaźń",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.isNil {
				got := parseEnvBlock(nil)
				if got != nil {
					t.Fatalf("expected nil, got: %v", got)
				}
				return
			}

			raw := createUTF16Block(tc.input)
			var ptr *uint16
			if len(raw) > 0 {
				ptr = &raw[0]
			}

			got := parseEnvBlock(ptr)
			if len(tc.expected) == 0 {
				if len(got) != 0 {
					t.Fatalf("expected empty/nil, got: %v", got)
				}
				return
			}

			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("parseEnvBlock mismatch:\ngot:  %v\nwant: %v", got, tc.expected)
			}
		})
	}
}
