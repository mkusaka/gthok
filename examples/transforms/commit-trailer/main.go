// commit-trailer appends a fixed trailer to explicitly supplied commit messages.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/protocol"
)

const trailer = "Origin: gthok-example"

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "commit-trailer:", err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer) error {
	var input protocol.Input
	if err := json.NewDecoder(in).Decode(&input); err != nil {
		return err
	}
	if input.SchemaVersion != 1 || input.Event != protocol.EventTransform {
		return fmt.Errorf("expected schemaVersion 1 transform event")
	}
	set := make(map[string]any)
	for i, op := range input.Operations {
		if op.Action != "history.commit.create" && op.Action != "history.commit.amend" {
			continue
		}
		if op.Texts == nil || op.Texts.Body == nil {
			return fmt.Errorf("operation %d requires an explicit -m or -F message", i)
		}
		body := op.Texts.Body
		if input.Redactions > 0 || body.Redactions > 0 || body.Truncated || body.Status != "" || slices.Contains(op.Capabilities, model.CapabilityEditor) || (body.Source != "inline" && body.Source != "file" && body.Source != "stdin") {
			return fmt.Errorf("operation %d message is unavailable or editor-generated", i)
		}
		value := strings.TrimRight(body.Value, "\r\n")
		if !strings.HasSuffix(value, "\n"+trailer) && value != trailer {
			set["/operations/"+strconv.Itoa(i)+"/texts/body/value"] = value + "\n\n" + trailer + "\n"
		}
	}
	if len(set) == 0 {
		return nil
	}
	return json.NewEncoder(out).Encode(protocol.TransformResponse{SchemaVersion: 1, Set: set})
}
