//go:build unix

package cli

import (
	"os"
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
)

func TestProtocolInputOnlyCapturedFromRegularFiles(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	pipe, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pipe.Close(); _ = writer.Close() }()
	for _, args := range [][]string{
		{"update-ref", "--stdin"}, {"cat-file", "--batch"}, {"cat-file", "--batch-check"}, {"cat-file", "--batch-command"},
		{"fast-import"}, {"credential", "fill"}, {"credential-store", "get"}, {"send-pack", "--stdin", "origin"},
		{"check-ignore", "--stdin"}, {"check-attr", "--stdin", "all"}, {"hash-object", "--stdin-paths"},
	} {
		if analysisNeedsStdin(args, pipe) || !analysisNeedsStdin(args, file) {
			t.Fatalf("unsafe acquisition: %q", args)
		}
		r := normalizeInvocation(&config.Config{}, model.Invocation{Argv: args, Stdin: model.StdinData{Status: model.StdinNotCaptured}})
		if r.Operations[0].UnknownReason != model.UnknownInputUnavailable {
			t.Fatalf("live protocol not marked input-unavailable: %q %+v", args, r)
		}
	}
}

func TestFiniteInputAcquisition(t *testing.T) {
	pipe, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pipe.Close(); _ = writer.Close() }()
	for _, args := range [][]string{
		{"commit", "-F-"}, {"tag", "-F-", "topic"}, {"notes", "add", "-F-"}, {"hash-object", "--stdin"},
		{"fetch", "--stdin", "origin"}, {"am"}, {"apply"}, {"am", "-"}, {"apply", "-"},
		{"notes", "copy", "--stdin"}, {"notes", "remove", "--stdin"},
		{"sparse-checkout", "set", "--stdin"}, {"sparse-checkout", "add", "--stdin"}, {"sparse-checkout", "check-rules"},
	} {
		if !analysisNeedsStdin(args, pipe) {
			t.Fatalf("finite input not requested: %q", args)
		}
	}
	for _, args := range [][]string{{"merge", "-F-", "topic"}, {"am", "--continue"}, {"am", "--abort"}, {"am", "--show-current-patch"}, {"apply", "--help"}, {"update-ref", "--stdin", "--help"}} {
		if analysisNeedsStdin(args, pipe) {
			t.Fatalf("non-reading mode requested input: %q", args)
		}
	}
}
