package policy

import (
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
)

func TestMatchInvocationEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern map[string]string
		env     map[string]string
		want    bool
	}{
		{"no criteria", nil, nil, true},
		{"exact", map[string]string{"WORKER": "1"}, map[string]string{"WORKER": "1"}, true},
		{"exact mismatch", map[string]string{"WORKER": "1"}, map[string]string{"WORKER": "0"}, false},
		{"set", map[string]string{"WORKER": "*"}, map[string]string{"WORKER": "1"}, true},
		{"set with slash", map[string]string{"WORKER": "*"}, map[string]string{"WORKER": "team/worker"}, true},
		{"set rejects absent", map[string]string{"WORKER": "*"}, nil, false},
		{"set rejects empty", map[string]string{"WORKER": "*"}, map[string]string{"WORKER": ""}, false},
		{"unset", map[string]string{"WORKER": ""}, nil, true},
		{"empty", map[string]string{"WORKER": ""}, map[string]string{"WORKER": ""}, true},
		{"unset rejects set", map[string]string{"WORKER": ""}, map[string]string{"WORKER": "1"}, false},
		{"glob", map[string]string{"WORKER": "worker-[12]?"}, map[string]string{"WORKER": "worker-2a"}, true},
		{"case sensitive", map[string]string{"WORKER": "worker-*"}, map[string]string{"WORKER": "Worker-1"}, false},
		{"glob slash boundary", map[string]string{"WORKER": "worker-*"}, map[string]string{"WORKER": "worker-a/b"}, false},
		{"invalid glob", map[string]string{"WORKER": "["}, map[string]string{"WORKER": "1"}, false},
		{"all entries", map[string]string{"WORKER": "1", "CI": "true"}, map[string]string{"WORKER": "1", "CI": "true"}, true},
		{"one entry fails", map[string]string{"WORKER": "1", "CI": "true"}, map[string]string{"WORKER": "1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := subject(operation("ref.update", "/repo", model.EffectWrite))
			s.Env = tc.env
			m := config.Match{Env: tc.pattern, Operations: config.StringList{"ref.*"}}
			if got, _ := Matches(m, nil, s); got != tc.want {
				t.Fatalf("Matches=%t, want %t", got, tc.want)
			}
			if got, _ := MatchMaybe(m, nil, s); got {
				t.Fatal("generic criteria cannot be unresolved")
			}
		})
	}
}
