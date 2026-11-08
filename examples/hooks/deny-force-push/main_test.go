package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
	"github.com/mkusaka/gthok/internal/protocol"
)

func TestForcePushHook(t *testing.T) {
	for _, tc := range []struct {
		args []string
		deny bool
	}{
		{[]string{"push", "--force", "origin", "HEAD:refs/heads/main"}, true},
		{[]string{"push", "origin", "+HEAD:refs/heads/release/v1"}, true},
		{[]string{"push", "--force-with-lease", "origin", "HEAD:refs/heads/main"}, true},
		{[]string{"push", "--force", "origin", "main"}, true},
		{[]string{"push", "--mirror", "origin"}, true},
		{[]string{"push", "--force", "origin", "HEAD:refs/heads/topic"}, false},
		{[]string{"push", "origin", "HEAD:refs/heads/main"}, false},
		{[]string{"push", "-nf", "origin", "HEAD:refs/heads/main"}, false},
	} {
		r := gitnormalize.Normalize(model.Invocation{Argv: tc.args})
		data, err := json.Marshal(protocol.Input{SchemaVersion: 1, Event: protocol.EventPre, Operations: r.Operations})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := run(bytes.NewReader(data), &out); err != nil {
			t.Fatal(err)
		}
		var response protocol.Response
		if err := json.Unmarshal(out.Bytes(), &response); err != nil || (response.Decision == protocol.DecisionDeny) != tc.deny {
			t.Fatalf("%q: %s, %v", tc.args, out.Bytes(), err)
		}
	}
	for _, data := range []string{"not JSON", `{"schemaVersion":2,"event":"pre"}`, `{"schemaVersion":1,"event":"post"}`} {
		if err := run(bytes.NewBufferString(data), &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
