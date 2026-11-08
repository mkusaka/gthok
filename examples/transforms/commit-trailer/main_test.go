package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
	"github.com/mkusaka/gthok/internal/protocol"
	"github.com/mkusaka/gthok/internal/transforms"
)

func TestCommitTrailerRoundTrip(t *testing.T) {
	for _, args := range [][]string{{"commit", "-m", "message"}, {"commit", "--amend", "-F", "message.txt"}, {"commit", "-F-"}} {
		inv := model.Invocation{Argv: args, Stdin: model.StdinData{Status: model.StdinCaptured, Data: []byte("message\n")}, ReadFile: func(string) ([]byte, error) { return []byte("message\n"), nil }}
		before := gitnormalize.Normalize(inv)
		data, err := json.Marshal(protocol.Input{SchemaVersion: 1, Event: protocol.EventTransform, Operations: before.Operations})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := run(bytes.NewReader(data), &out); err != nil {
			t.Fatal(err)
		}
		response, err := protocol.ParseTransformResponse(out.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		_, after, cleanup, err := transforms.Apply(inv, before, response)
		cleanup()
		if err != nil || !strings.HasSuffix(after.Operations[0].Texts.Body.Value, trailer+"\n") {
			t.Fatal(after, err)
		}
		data, err = json.Marshal(protocol.Input{SchemaVersion: 1, Event: protocol.EventTransform, Operations: after.Operations})
		if err != nil {
			t.Fatal(err)
		}
		out.Reset()
		if err := run(bytes.NewReader(data), &out); err != nil || out.Len() != 0 {
			t.Fatal("trailer not idempotent", out.String(), err)
		}
	}
}

func TestRedactedCommitMessage(t *testing.T) {
	input := protocol.Input{SchemaVersion: 1, Event: protocol.EventTransform, Redactions: 1, Operations: []model.Operation{{Action: "history.commit.create", Texts: &model.Texts{Body: &model.Text{Value: "masked", Source: "inline"}}}}}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(bytes.NewReader(data), &bytes.Buffer{}); err == nil {
		t.Fatal("accepted redacted input for write-back")
	}
}

func TestUnavailableCommitMessage(t *testing.T) {
	for _, args := range [][]string{{"commit"}, {"commit", "-F-"}, {"commit", "--edit", "-m", "message"}} {
		r := gitnormalize.Normalize(model.Invocation{Argv: args})
		data, err := json.Marshal(protocol.Input{SchemaVersion: 1, Event: protocol.EventTransform, Operations: r.Operations})
		if err != nil {
			t.Fatal(err)
		}
		if err := run(bytes.NewReader(data), &bytes.Buffer{}); err == nil {
			t.Fatal("accepted unavailable/editor message", args)
		}
	}
}
