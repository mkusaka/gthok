// audit-history records requested history rewrites and Git's final status.
// A successful command is not proof of which refs actually changed.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/protocol"
)

func main() {
	executable, err := os.Executable()
	if err == nil {
		err = run(os.Stdin, filepath.Join(filepath.Dir(executable), "..", "logs", "history-audit.jsonl"))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit-history:", err)
		os.Exit(1)
	}
}

func run(in io.Reader, path string) error {
	var input protocol.Input
	if err := json.NewDecoder(in).Decode(&input); err != nil {
		return err
	}
	if input.SchemaVersion != 1 || input.Event != protocol.EventPost || input.Result == nil {
		return fmt.Errorf("expected schemaVersion 1 post event with result")
	}
	actions := []string{}
	for _, op := range input.Operations {
		if op.Effect == model.EffectRead || op.Execution == model.ExecutionSkipped || op.Execution == model.ExecutionNotSelected {
			continue
		}
		if op.Action == "history.commit.amend" || strings.HasPrefix(op.Action, "history.rebase.") || strings.HasPrefix(op.Action, "worktree.reset.") || strings.HasPrefix(op.Action, "remote.push.") && (op.Parameters["forceMode"] == "unconditional" || op.Parameters["forceMode"] == "lease") {
			actions = append(actions, op.Action)
		}
	}
	if len(actions) == 0 {
		return nil
	}
	record := struct {
		Time     time.Time `json:"time"`
		EventID  string    `json:"eventId"`
		Actions  []string  `json:"actions"`
		ExitCode int       `json:"exitCode"`
		Signal   string    `json:"signal,omitempty"`
	}{time.Now().UTC(), input.EventID, actions, input.Result.ExitCode, input.Result.Signal}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	// shortcut: one local append, add rotation and durability for production audit storage.
	line = append(line, '\n')
	n, err := file.Write(line)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if n != len(line) {
		return io.ErrShortWrite
	}
	return closeErr
}
