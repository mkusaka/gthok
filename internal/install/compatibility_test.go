package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitVersionFormats(t *testing.T) {
	for _, tc := range []struct {
		out, want string
	}{
		{"git version 2.56.0\n", "2.56.0"},
		{"git version 2.56.0", "2.56.0"},
		{"git version 2.56.0 (Apple Git-123)\n", "2.56.0"},
		{"git version 2.56.0-rc1\n", "2.56.0-rc1"},
		{"git version 2.56.0+vendor\n", "2.56.0+vendor"},
		{"git version 2.56.0.1.gabcdef\n", "2.56.0.1.gabcdef"},
		{"git version 2.55.9\n", "2.55.9"},
		{"git version 2.57.0\n", "2.57.0"},
		{"git version 2.56.01\n", "2.56.01"},
		{"git version 2.56\n", ""},
		{"git version 2.56.0garbage\n", ""},
		{"unexpected\ngit version 2.56.0\n", ""},
	} {
		t.Run(strings.TrimSpace(tc.out), func(t *testing.T) {
			got, err := parseGitVersion(tc.out)
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("version=%q err=%v, want %q", got, err, tc.want)
			}
			status, detail := gitVersionStatus(tc.out)
			compatibility := "unverified"
			if tc.want == GitBaseline {
				compatibility = "verified"
			}
			if !strings.Contains(detail, "compatibility="+compatibility) || (status == StatusInfo) != (compatibility == "verified") {
				t.Fatalf("status=%s detail=%s", status, detail)
			}
		})
	}
}

func TestGitVersionProbeDoesNotCacheOrReadStdin(t *testing.T) {
	root := t.TempDir()
	path, calls := filepath.Join(root, "git"), filepath.Join(root, "calls")
	for _, version := range []string{"2.56.0", "2.57.1"} {
		executable(t, path, "test \"$#\" = 1 && test \"$1\" = --version && test \"$GTHOK_VERSION_QUERY\" = 1 || exit 2\n"+
			"if read -r line; then exit 3; fi\nprintf 'probe\\n' >> '"+calls+"'\nprintf 'git version "+version+"\\n'")
		got, err := GitVersion(path, []string{"PATH=/nonexistent"})
		if err != nil || got != version {
			t.Fatalf("version=%s err=%v, want %s", got, err, version)
		}
	}
	data, err := os.ReadFile(calls)
	if err != nil || string(data) != "probe\nprobe\n" {
		t.Fatalf("probes=%q err=%v", data, err)
	}
}

func TestDoctorReportsCompatibility(t *testing.T) {
	for _, version := range []string{"2.56.0", "2.55.1", "2.57.0"} {
		t.Run(version, func(t *testing.T) {
			o := doctorFixture(t)
			real := filepath.Join(filepath.Dir(o.Self), "real", "git")
			calls := filepath.Join(filepath.Dir(o.Self), "version-calls")
			executable(t, real, "printf 'probe\\n' >> '"+calls+"'\nprintf 'git version "+version+"\\n'")
			checks := Checks(o)
			if got := checkNamed(t, checks, "real-git"); got.Detail != real {
				t.Fatalf("real git=%+v", got)
			}
			want := "unverified"
			if version == GitBaseline {
				want = "verified"
			}
			got := checkNamed(t, checks, "git-version")
			if !strings.Contains(got.Detail, version) || !strings.Contains(got.Detail, "compatibility="+want) {
				t.Fatalf("version=%+v", got)
			}
			data, err := os.ReadFile(calls)
			if err != nil || string(data) != "probe\n" {
				t.Fatalf("probes=%q err=%v", data, err)
			}
		})
	}
}
