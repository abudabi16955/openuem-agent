package bitlocker

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

var (
	// ErrBitLockerNotSupported indicates BitLocker is not supported on the current platform.
	ErrBitLockerNotSupported = errors.New("BitLocker is not supported on this platform")
	// ErrInvalidRecoveryKey indicates an invalid recovery password format.
	ErrInvalidRecoveryKey = errors.New("invalid BitLocker recovery key")
)

// KeyProtector represents a BitLocker numerical recovery password protector.
type KeyProtector struct {
	DriveLetter      string    `json:"drive_letter"`
	KeyID            string    `json:"key_id"`
	RecoveryPassword string    `json:"recovery_password"`
	CreatedAt        time.Time `json:"created_at"`
}

// VolumeInfo represents a Windows volume and its BitLocker status.
type VolumeInfo struct {
	DriveLetter      string         `json:"drive_letter"`
	ProtectionStatus int            `json:"protection_status"` // 0: Unprotected, 1: Protected, 2: Unknown
	Protectors       []KeyProtector `json:"protectors"`
}

// CleanKeyID normalizes a Key ID to uppercase with standard enclosing braces {GUID}.
func CleanKeyID(keyID string) string {
	s := strings.TrimSpace(keyID)
	s = strings.ToUpper(s)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "{") && !strings.HasSuffix(s, "}") {
		s = "{" + s + "}"
	}
	return s
}

// NormalizeRecoveryPassword normalizes and validates a 48-digit numerical recovery password.
// Standard BitLocker format is 8 groups of 6 digits separated by hyphens (e.g. 111111-222222-333333-444444-555555-666666-777777-888888).
func NormalizeRecoveryPassword(password string) (string, error) {
	var digits strings.Builder
	for _, r := range strings.TrimSpace(password) {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
		} else if r != '-' && r != ' ' {
			return "", fmt.Errorf("%w: invalid character %q in recovery password", ErrInvalidRecoveryKey, r)
		}
	}

	raw := digits.String()
	if len(raw) != 48 {
		return "", fmt.Errorf("%w: recovery password must contain exactly 48 digits, got %d", ErrInvalidRecoveryKey, len(raw))
	}

	var groups []string
	for i := 0; i < 48; i += 6 {
		groups = append(groups, raw[i:i+6])
	}
	return strings.Join(groups, "-"), nil
}
