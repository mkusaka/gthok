package config

import (
	"strings"
	"testing"
)

func TestGitCompatibilitySetting(t *testing.T) {
	for _, mode := range []string{"", "conservative", "verified-only"} {
		body := "version: 1\n"
		if mode != "" {
			body += "runtime: {gitCompatibility: " + mode + "}\n"
		}
		c, err := Parse([]byte(body), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		want := mode
		if want == "" {
			want = "conservative"
		}
		if c.Runtime.GitCompatibility != want {
			t.Fatalf("mode=%q, want %q", c.Runtime.GitCompatibility, want)
		}
	}
	if _, err := Parse([]byte("version: 1\nruntime: {gitCompatibility: ignore}\n"), t.TempDir()); err == nil || !strings.Contains(err.Error(), "runtime.gitCompatibility") {
		t.Fatalf("invalid setting accepted: %v", err)
	}
}
