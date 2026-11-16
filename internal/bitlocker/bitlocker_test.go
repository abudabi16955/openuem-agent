package bitlocker

import (
	"encoding/json"
	"testing"
	"time"

	openuem_nats "github.com/open-uem/nats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanKeyID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"   ", ""},
		{"{12345678-ABCD-1234-EF01-1234567890AB}", "{12345678-ABCD-1234-EF01-1234567890AB}"},
		{"12345678-abcd-1234-ef01-1234567890ab", "{12345678-ABCD-1234-EF01-1234567890AB}"},
		{"  {abcdef12-3456-7890-abcd-ef1234567890}  ", "{ABCDEF12-3456-7890-ABCD-EF1234567890}"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			res := CleanKeyID(tc.input)
			assert.Equal(t, tc.expected, res)
		})
	}
}

func TestNormalizeRecoveryPassword(t *testing.T) {
	t.Run("valid 48-digit unformatted", func(t *testing.T) {
		input := "111111222222333333444444555555666666777777888888"
		expected := "111111-222222-333333-444444-555555-666666-777777-888888"
		res, err := NormalizeRecoveryPassword(input)
		require.NoError(t, err)
		assert.Equal(t, expected, res)
	})

	t.Run("valid 48-digit already formatted with hyphens", func(t *testing.T) {
		input := "111111-222222-333333-444444-555555-666666-777777-888888"
		res, err := NormalizeRecoveryPassword(input)
		require.NoError(t, err)
		assert.Equal(t, input, res)
	})

	t.Run("valid with spaces and hyphens", func(t *testing.T) {
		input := "  111111-222222 333333-444444 555555-666666 777777-888888  "
		expected := "111111-222222-333333-444444-555555-666666-777777-888888"
		res, err := NormalizeRecoveryPassword(input)
		require.NoError(t, err)
		assert.Equal(t, expected, res)
	})

	t.Run("invalid length - too short", func(t *testing.T) {
		input := "111111-222222-333333-444444-555555-666666-777777-88888"
		_, err := NormalizeRecoveryPassword(input)
		assert.ErrorIs(t, err, ErrInvalidRecoveryKey)
	})

	t.Run("invalid length - too long", func(t *testing.T) {
		input := "111111-222222-333333-444444-555555-666666-777777-8888889"
		_, err := NormalizeRecoveryPassword(input)
		assert.ErrorIs(t, err, ErrInvalidRecoveryKey)
	})

	t.Run("invalid characters", func(t *testing.T) {
		input := "111111-222222-333333-444444-555555-666666-777777-88888X"
		_, err := NormalizeRecoveryPassword(input)
		assert.ErrorIs(t, err, ErrInvalidRecoveryKey)
	})
}

func TestBitLockerEscrowRequestSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	req := openuem_nats.BitLockerEscrowRequest{
		AgentID:          "agent-uuid-1234",
		DriveLetter:      "C:",
		KeyID:            "{ABCD-1234}",
		RecoveryPassword: "111111-222222-333333-444444-555555-666666-777777-888888",
		CreatedAt:        now,
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var unmarshaled openuem_nats.BitLockerEscrowRequest
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, req.AgentID, unmarshaled.AgentID)
	assert.Equal(t, req.DriveLetter, unmarshaled.DriveLetter)
	assert.Equal(t, req.KeyID, unmarshaled.KeyID)
	assert.Equal(t, req.RecoveryPassword, unmarshaled.RecoveryPassword)
}

func TestEscrowService_DisconnectedNATS(t *testing.T) {
	svc := NewEscrowService(nil, "agent-123")
	count, err := svc.CheckAndEscrowKeys(false)
	assert.Error(t, err)
	assert.Equal(t, 0, count)
}
