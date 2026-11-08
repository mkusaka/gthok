package policy

import (
	"testing"

	"github.com/mkusaka/gthok/internal/config"
	"github.com/mkusaka/gthok/internal/model"
)

func TestGitGenericCriteriaStayOnOneOperation(t *testing.T) {
	write := operation("remote.push.update", "/repo", model.EffectWrite)
	write.Parameters = map[string]any{"forceMode": "unconditional"}
	read := operation("config.get", "/repo", model.EffectRead)
	s := subject(write, read)
	m := config.Match{Operations: config.StringList{"remote.push.*"}, Effects: config.StringList{"read"}}
	if yes, _ := Matches(m, nil, s); yes {
		t.Fatal("criteria matched different operations")
	}
	m.Effects = config.StringList{"write"}
	m.Parameters = map[string]string{"forceMode": "unconditional"}
	if yes, indexes := Matches(m, nil, s); !yes || len(indexes) != 1 || indexes[0] != 0 {
		t.Fatal("same-operation criteria did not match")
	}
	unless := []config.Match{{Parameters: map[string]string{"forceMode": "unconditional"}}}
	if yes, _ := Matches(m, unless, s); yes {
		t.Fatal("definite unless did not exclude")
	}
	s.Result.Operations[0].Execution = model.ExecutionSkipped
	if yes, _ := Matches(m, nil, s); yes {
		t.Fatal("skipped operation matched")
	}
}
