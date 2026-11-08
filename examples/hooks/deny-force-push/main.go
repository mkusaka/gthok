// deny-force-push rejects force pushes to main or release branches.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/protocol"
)

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "deny-force-push:", err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer) error {
	var input protocol.Input
	if err := json.NewDecoder(in).Decode(&input); err != nil {
		return err
	}
	if input.SchemaVersion != 1 || input.Event != protocol.EventPre {
		return fmt.Errorf("expected schemaVersion 1 pre event")
	}
	response := protocol.Response{SchemaVersion: 1, Decision: protocol.DecisionAbstain}
	for _, op := range input.Operations {
		if !strings.HasPrefix(op.Action, "remote.push.") || op.Effect == model.EffectRead || op.Execution == model.ExecutionSkipped || op.Execution == model.ExecutionNotSelected {
			continue
		}
		if op.Parameters["forceMode"] != "unconditional" && op.Parameters["forceMode"] != "lease" {
			continue
		}
		refs := op.Target.Refs
		protected := refs == nil || !refs.Complete || refs.Kind == model.SetUnknown || refs.Kind == model.SetPattern
		if refs != nil {
			for _, ref := range refs.Values {
				protected = protected || ref == "refs/heads/main" || strings.HasPrefix(ref, "refs/heads/release/")
			}
		}
		if protected {
			response.Decision, response.Reason = protocol.DecisionDeny, "force push may affect a protected branch"
			break
		}
	}
	return json.NewEncoder(out).Encode(response)
}
