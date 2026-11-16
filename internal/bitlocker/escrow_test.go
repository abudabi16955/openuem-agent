package bitlocker

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	openuem_nats "github.com/open-uem/nats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startTestNatsServer(t *testing.T) (string, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port for nats: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	cmd := exec.Command("nats-server", "-a", "127.0.0.1", "-p", strconv.Itoa(port))
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start nats-server: %v", err)
	}

	url := fmt.Sprintf("nats://127.0.0.1:%d", port)
	ready := false
	for i := 0; i < 50; i++ {
		time.Sleep(50 * time.Millisecond)
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			conn.Close()
			ready = true
			break
		}
	}
	if !ready {
		_ = cmd.Process.Kill()
		t.Fatalf("nats-server did not become ready in time on %s", url)
	}

	cleanup := func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	return url, cleanup
}

func setupMockKeys(t *testing.T, keyID string) func() {
	t.Helper()
	orig := getRecoveryKeysFunc
	getRecoveryKeysFunc = func() ([]KeyProtector, error) {
		return []KeyProtector{
			{
				DriveLetter:      "C:",
				KeyID:            keyID,
				RecoveryPassword: "111111-222222-333333-444444-555555-666666-777777-888888",
				CreatedAt:        time.Now().UTC(),
			},
		}, nil
	}
	return func() {
		getRecoveryKeysFunc = orig
	}
}

func TestEscrowService_Success_MarksRegistry(t *testing.T) {
	ResetMockRegistry()
	natsURL, cleanupNATS := startTestNatsServer(t)
	defer cleanupNATS()

	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()

	agentUUID := "agent-success-test"
	cleanID := "{11111111-2222-3333-4444-555555555555}"
	cleanupKeys := setupMockKeys(t, cleanID)
	defer cleanupKeys()

	// Server handler that replies with success ACK
	subject := openuem_nats.BitLockerEscrowSubject(agentUUID)
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		resp := openuem_nats.NewBitLockerEscrowSuccessResponse()
		data, _ := json.Marshal(resp)
		_ = msg.Respond(data)
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()
	_ = nc.Flush()

	svc := NewEscrowService(nc, agentUUID)
	count, err := svc.CheckAndEscrowKeys(false)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "expected 1 key to be successfully escrowed")

	// Verify registry is marked
	isEscrowed, err := IsKeyEscrowed(cleanID)
	require.NoError(t, err)
	assert.True(t, isEscrowed, "registry should have recorded the key after ACK")
}

func TestEscrowService_NoACK_Timeout_RegistryRemainsClean(t *testing.T) {
	ResetMockRegistry()
	natsURL, cleanupNATS := startTestNatsServer(t)
	defer cleanupNATS()

	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()

	agentUUID := "agent-no-ack-test"
	cleanID := "{22222222-3333-4444-5555-666666666666}"
	cleanupKeys := setupMockKeys(t, cleanID)
	defer cleanupKeys()

	// Server subscriber consumes message but NEVER sends ACK (simulating hang / crash / drop)
	subject := openuem_nats.BitLockerEscrowSubject(agentUUID)
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		// No msg.Respond!
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()
	_ = nc.Flush()

	svc := NewEscrowService(nc, agentUUID)
	svc.SetTimeout(100 * time.Millisecond)

	count, err := svc.CheckAndEscrowKeys(false)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "expected 0 keys escrowed when server does not ACK")

	// Crucial: registry MUST remain clean so key is retried on subsequent cycles
	isEscrowed, err := IsKeyEscrowed(cleanID)
	require.NoError(t, err)
	assert.False(t, isEscrowed, "registry MUST remain clean when no ACK is received")
}

func TestEscrowService_ServerError_RegistryRemainsClean(t *testing.T) {
	ResetMockRegistry()
	natsURL, cleanupNATS := startTestNatsServer(t)
	defer cleanupNATS()

	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()

	agentUUID := "agent-err-resp-test"
	cleanID := "{33333333-4444-5555-6666-777777777777}"
	cleanupKeys := setupMockKeys(t, cleanID)
	defer cleanupKeys()

	// Server replies with explicit failure
	subject := openuem_nats.BitLockerEscrowSubject(agentUUID)
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		resp := openuem_nats.NewBitLockerEscrowErrorResponse("database transaction rollback")
		data, _ := json.Marshal(resp)
		_ = msg.Respond(data)
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()
	_ = nc.Flush()

	svc := NewEscrowService(nc, agentUUID)
	svc.SetTimeout(500 * time.Millisecond)

	count, err := svc.CheckAndEscrowKeys(false)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "expected 0 keys escrowed when server returns error response")

	// Registry must remain clean
	isEscrowed, err := IsKeyEscrowed(cleanID)
	require.NoError(t, err)
	assert.False(t, isEscrowed, "registry MUST remain clean when server returns error")
}

func TestEscrowService_InvalidServerResponse_RegistryRemainsClean(t *testing.T) {
	ResetMockRegistry()
	natsURL, cleanupNATS := startTestNatsServer(t)
	defer cleanupNATS()

	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()

	agentUUID := "agent-corrupt-resp-test"
	cleanID := "{44444444-5555-6666-7777-888888888888}"
	cleanupKeys := setupMockKeys(t, cleanID)
	defer cleanupKeys()

	// Server replies with garbage bytes
	subject := openuem_nats.BitLockerEscrowSubject(agentUUID)
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		_ = msg.Respond([]byte("not valid json"))
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()
	_ = nc.Flush()

	svc := NewEscrowService(nc, agentUUID)
	svc.SetTimeout(500 * time.Millisecond)

	count, err := svc.CheckAndEscrowKeys(false)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "expected 0 keys escrowed when server returns garbage response")

	// Registry must remain clean
	isEscrowed, err := IsKeyEscrowed(cleanID)
	require.NoError(t, err)
	assert.False(t, isEscrowed, "registry MUST remain clean when server response is corrupt")
}
