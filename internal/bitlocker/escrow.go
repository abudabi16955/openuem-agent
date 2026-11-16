package bitlocker

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	openuem_nats "github.com/open-uem/nats"
)

var getRecoveryKeysFunc = GetRecoveryKeys

// EscrowService handles discovering BitLocker keys and escrowing them to NATS.
type EscrowService struct {
	nc        *nats.Conn
	agentUUID string
	timeout   time.Duration
}

// NewEscrowService creates a new instance of EscrowService.
func NewEscrowService(nc *nats.Conn, agentUUID string) *EscrowService {
	return &EscrowService{
		nc:        nc,
		agentUUID: agentUUID,
		timeout:   10 * time.Second,
	}
}

// SetTimeout sets custom request timeout for testing.
func (s *EscrowService) SetTimeout(t time.Duration) {
	s.timeout = t
}

// CheckAndEscrowKeys retrieves recovery keys and escrows any key that hasn't been recorded yet.
// If force is true, all active recovery keys are escrowed regardless of registry cache.
func (s *EscrowService) CheckAndEscrowKeys(force bool) (int, error) {
	if s.nc == nil || !s.nc.IsConnected() {
		return 0, errors.New("NATS connection is not ready")
	}

	keys, err := getRecoveryKeysFunc()
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve BitLocker recovery keys: %w", err)
	}

	if len(keys) == 0 {
		log.Println("[INFO]: no BitLocker recovery keys found for escrow")
		return 0, nil
	}

	escrowedCount := 0
	timeout := s.timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	for _, key := range keys {
		cleanID := CleanKeyID(key.KeyID)
		if !force {
			alreadyEscrowed, err := IsKeyEscrowed(cleanID)
			if err == nil && alreadyEscrowed {
				log.Printf("[DEBUG]: BitLocker key %s for drive %s already escrowed, skipping", cleanID, key.DriveLetter)
				continue
			}
		}

		req := openuem_nats.BitLockerEscrowRequest{
			AgentID:          s.agentUUID,
			DriveLetter:      key.DriveLetter,
			KeyID:            cleanID,
			RecoveryPassword: key.RecoveryPassword,
			CreatedAt:        key.CreatedAt,
		}

		data, err := json.Marshal(req)
		if err != nil {
			log.Printf("[ERROR]: could not marshal BitLocker escrow request for key %s: %v", cleanID, err)
			continue
		}

		subject := openuem_nats.BitLockerEscrowSubject(s.agentUUID)
		respMsg, err := s.nc.Request(subject, data, timeout)
		if err != nil {
			log.Printf("[ERROR]: failed to request BitLocker escrow for key %s: %v", cleanID, err)
			continue
		}

		var escrowResp openuem_nats.BitLockerEscrowResponse
		if err := json.Unmarshal(respMsg.Data, &escrowResp); err != nil {
			log.Printf("[ERROR]: failed to unmarshal BitLocker escrow response for key %s: %v", cleanID, err)
			continue
		}

		if !escrowResp.Success {
			log.Printf("[ERROR]: BitLocker escrow rejected by server for key %s: %s", cleanID, escrowResp.Error)
			continue
		}

		if err := MarkKeyEscrowed(cleanID, key.DriveLetter); err != nil {
			log.Printf("[WARN]: could not mark BitLocker key %s in registry: %v", cleanID, err)
		}

		escrowedCount++
		log.Printf("[INFO]: BitLocker recovery key %s for drive %s escrowed successfully (force=%v)", cleanID, key.DriveLetter, force)
	}

	return escrowedCount, nil
}
