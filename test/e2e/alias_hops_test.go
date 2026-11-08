//go:build unix

package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

func TestGitAliasHopsAndExecutablePrecedence(t *testing.T) {
	for _, tt := range []struct {
		name       string
		command    string
		aliases    map[string]string
		executable string
	}{
		{
			name: "alias-provided override without executable", command: "lg2",
			aliases: map[string]string{"lg2": "-c alias.x=status x"},
		},
		{
			name: "alias-provided override with executable", command: "lg2",
			aliases: map[string]string{"lg2": "-c alias.x=status x"}, executable: "x",
		},
		{
			name: "ordinary intermediate executable", command: "a",
			aliases: map[string]string{"a": "b", "b": "c", "c": "status"}, executable: "b",
		},
		{
			name: "inline intermediate executable", command: "a",
			aliases: map[string]string{"a": "-c alias.b=c -c alias.c=status b"}, executable: "b",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, real := gitFixture(t)
			for name, value := range tt.aliases {
				s.run(real, nil, "config", "alias."+name, value).code(t, 0)
			}
			marker := filepath.Join(s.root, "external-ran")
			s.env["ALIAS_MARKER"] = marker
			if tt.executable != "" {
				body := []byte("#!/bin/sh\nprintf invoked > \"$ALIAS_MARKER\"\nprintf 'external\\n'\n")
				if err := os.WriteFile(filepath.Join(s.bin, "git-"+tt.executable), body, 0700); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"-C", s.cwd, tt.command, "--short"}
			r := normalizeGitFixture(t, s, args...)
			op := r.Operations[0]
			if tt.executable == "" {
				if op.Action != "status.show" || op.UnknownReason != model.UnknownNone || r.Source.Alias != tt.command {
					t.Fatalf("alias classification: %+v", r)
				}
			} else if op.UnknownReason != model.UnknownExternal || !reflect.DeepEqual(op.Origin.Command, []string{tt.executable}) || r.Classification.RetrySafe {
				t.Fatalf("executable classification: %+v", r)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("normalization ran an external command: %v", err)
			}
			direct, wrapped := s.run(real, nil, args...), s.git(nil, args...)
			direct.code(t, 0)
			wrapped.code(t, 0)
			if !bytes.Equal(direct.stdout, wrapped.stdout) {
				t.Fatalf("execution changed: direct=%q wrapped=%q", direct.stdout, wrapped.stdout)
			}
			if tt.executable != "" && string(direct.stdout) != "external\n" {
				t.Fatalf("Git did not select the executable: %q", direct.stdout)
			}
		})
	}
}
