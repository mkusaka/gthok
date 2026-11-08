//go:build unix

package e2e

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestManagementDocs(t *testing.T) {
	s := newSandbox(t)
	// Discovery must not depend on valid configuration or an installed git.
	if err := os.WriteFile(s.cfg, []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	s.env["GTHOK_REAL_GIT"] = "/no-such-git"
	text := s.management(nil, "docs")
	text.code(t, 0)
	if !bytes.Contains(text.stdout, []byte("  security/project-configuration-trust  Project configuration trust — ")) || len(text.stderr) != 0 {
		t.Fatalf("docs tree stdout=%s stderr=%s", text.stdout, text.stderr)
	}
	tree := s.management(nil, "docs", "--json")
	tree.code(t, 0)
	var sections []struct {
		ID, Title, Description, File string
		Depth                        int
	}
	decodeJSON(t, tree.stdout, &sections)
	ids := make(map[string]bool)
	for _, section := range sections {
		if section.ID == "" || section.Title == "" || ids[section.ID] || strings.Contains(section.ID, ".") {
			t.Fatalf("invalid or duplicate docs section: %+v", section)
		}
		ids[section.ID] = true
		if section.File != "README.md" && section.File != "skills/gthok/SKILL.md" && (!strings.HasPrefix(section.File, "docs/") || strings.Count(section.File, "/") != 1) {
			t.Fatalf("unexpected embedded file: %q", section.File)
		}
		if section.ID == "security/project-configuration-trust" && section.Depth != 1 {
			t.Fatalf("incorrect heading depth: %+v", section)
		}
	}
	for _, id := range []string{"readme", "security", "security/project-configuration-trust", "hook-protocol/parallel-hooks", "skill"} {
		if !ids[id] {
			t.Fatalf("missing docs id %q", id)
		}
	}
	section := s.management(nil, "docs", "security/project-configuration-trust")
	section.code(t, 0)
	if !bytes.HasPrefix(section.stdout, []byte("## Project configuration trust\n")) || !bytes.Contains(section.stdout, []byte("### Transform trust and failures\n")) || bytes.Contains(section.stdout, []byte("## Redaction rules and limits\n")) || len(section.stderr) != 0 {
		t.Fatalf("incorrect section extraction: stdout=%s stderr=%s", section.stdout, section.stderr)
	}
	all := s.management(nil, "docs", "--all")
	all.code(t, 0)
	for _, file := range []string{"README.md", "docs/architecture.md", "docs/compatibility.md", "docs/hook-protocol.md", "docs/normalization.md", "docs/security.md", "skills/gthok/SKILL.md"} {
		if !bytes.Contains(all.stdout, []byte("<!-- file: "+file+" -->\n")) {
			t.Fatalf("missing full-docs marker for %s", file)
		}
	}
	if bytes.Contains(all.stdout, []byte("<!-- file: docs/notes/")) || len(all.stderr) != 0 {
		t.Fatalf("full docs include notes or errors: %s", all.stderr)
	}
	s.absentRecord()
}

func TestManagementDocsSearchAndUnknown(t *testing.T) {
	s := newSandbox(t)
	result := s.management(nil, "docs", "search", "parallel hooks")
	result.code(t, 0)
	var matches []struct {
		ID, Title, Description, URL string
		Score                       float64
	}
	decodeJSON(t, result.stdout, &matches)
	if len(matches) == 0 || len(matches) > 5 || matches[0].ID != "hook-protocol/parallel-hooks" || matches[0].Title == "" || matches[0].Description == "" || matches[0].Score <= 0 {
		t.Fatalf("unexpected search matches: %+v", matches)
	}
	if matches[0].URL != "https://github.com/mkusaka/gthok/blob/main/docs/hook-protocol.md#parallel-hooks" || len(result.stderr) != 0 {
		t.Fatalf("unexpected search URL/stderr: %+v %s", matches, result.stderr)
	}
	empty := s.management(nil, "docs", "search", "zzzznonexistenttermzzzz")
	empty.code(t, 0)
	if strings.TrimSpace(string(empty.stdout)) != "[]" {
		t.Fatalf("no matches must be a JSON array: %s", empty.stdout)
	}
	unknown := s.management(nil, "docs", "security/project-configuration-trus")
	unknown.code(t, 1)
	if len(unknown.stdout) != 0 || !bytes.HasPrefix(unknown.stderr, []byte("gthok: unknown docs id \"security/project-configuration-trus\"")) || !bytes.Contains(unknown.stderr, []byte("security/project-configuration-trust")) {
		t.Fatalf("unknown id stdout=%q stderr=%q", unknown.stdout, unknown.stderr)
	}
	for _, args := range [][]string{
		{"docs", "search"}, {"docs", "search", "parallel", "hooks"},
		{"docs", "readme", "security"}, {"docs", "--all", "readme"},
		{"docs", "--all", "--json"},
	} {
		bad := s.management(nil, args...)
		bad.code(t, 2)
		if len(bad.stdout) != 0 || !bytes.HasPrefix(bad.stderr, []byte("gthok:")) {
			t.Fatalf("usage %q stdout=%q stderr=%q", args, bad.stdout, bad.stderr)
		}
	}
	s.absentRecord()
}

func TestDocsSkillSourceURL(t *testing.T) {
	s := newSandbox(t)
	result := s.management(nil, "docs", "skill")
	result.code(t, 0)
	// e2e builds use a non-release version, so the URL points at main.
	if !bytes.HasPrefix(result.stdout, []byte("<!-- Agent skill source for this gthok version: https://github.com/mkusaka/gthok/blob/main/skills/gthok/SKILL.md ")) {
		t.Fatalf("skill header: %.200s", result.stdout)
	}
	if bytes.Contains(s.management(nil, "docs", "readme").stdout, []byte("Agent skill source")) {
		t.Fatal("non-skill section got the skill header")
	}
}
