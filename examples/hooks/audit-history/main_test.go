package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/protocol"
)

func TestAuditHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	input := protocol.Input{SchemaVersion: 1, Event: protocol.EventPost, EventID: "test-event", Result: &protocol.ExecResult{ExitCode: 3}, Operations: []model.Operation{
		{Action: "history.commit.amend", Texts: &model.Texts{Body: &model.Text{Value: "private message"}}},
		{Action: "history.rebase.start", Execution: model.ExecutionSkipped},
		{Action: "status.show"},
		{Action: "history.rebase.show-current-patch", Effect: model.EffectRead},
		{Action: "remote.push.update", Effect: model.EffectRead, Parameters: map[string]any{"forceMode": "unconditional"}},
	}}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := run(bytes.NewReader(data), path); err != nil {
			t.Fatal(err)
		}
	}
	log, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 2 || strings.Contains(string(log), "private message") || strings.Contains(string(log), "rebase") {
		t.Fatal(string(log))
	}
	var record struct {
		Actions  []string `json:"actions"`
		ExitCode int      `json:"exitCode"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil || len(record.Actions) != 1 || record.ExitCode != 3 {
		t.Fatal(record, err)
	}
	if err := run(strings.NewReader(`{"schemaVersion":1,"event":"post"}`), path); err == nil {
		t.Fatal("accepted missing result")
	}
	if err := run(bytes.NewReader(data), filepath.Join(path, "missing")); err == nil {
		t.Fatal("ignored write failure")
	}
}
