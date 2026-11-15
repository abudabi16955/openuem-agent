//go:build integration

package agent

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	wire "github.com/open-uem/nats"
)

// Exercises the production sender without installing or starting the endpoint service.
func TestTaskStartReportTransportIntegration(t *testing.T) {
	url := os.Getenv("OPENUEM_TEST_NATS")
	if url == "" {
		t.Skip("set OPENUEM_TEST_NATS to an isolated broker")
	}
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	got := make(chan wire.TaskStartReport, 1)
	s, err := nc.Subscribe("wingetcfg.task_started", func(m *nats.Msg) {
		var r wire.TaskStartReport
		if err := json.Unmarshal(m.Data, &r); err != nil {
			t.Error(err)
		}
		got <- r
		if err := m.Respond(nil); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Unsubscribe()
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	a := &Agent{NATSConnection: nc}
	want := wire.TaskStartReport{ProfileID: 42, TaskID: 73, AgentID: "transport-fixture", Name: "task_73_1", StartTime: "2026-10-29T06:00:00Z"}
	if err := a.SendTaskStartReport(&want); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-got:
		if r != want {
			t.Fatalf("wire fields changed: got %+v want %+v", r, want)
		}
	case <-time.After(time.Second):
		t.Fatal("no start report received")
	}
}
