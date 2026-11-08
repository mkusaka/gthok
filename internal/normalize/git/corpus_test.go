package git

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mkusaka/gthok/internal/model"
)

type corpusCase struct {
	Name    string            `json:"name"`
	Argv    []string          `json:"argv"`
	Env     map[string]string `json:"env,omitempty"`
	Aliases map[string]string `json:"aliases,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Stdin   *string           `json:"stdin,omitempty"`
	Want    map[string]any    `json:"want"`
}

func TestNormalizationCorpus(t *testing.T) {
	files, err := filepath.Glob("../../../testdata/corpus/*.json")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cases []corpusCase
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cases); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatalf("%s: trailing JSON: %v", path, err)
		}
		seen := make(map[string]bool)
		for _, tc := range cases {
			count++
			if tc.Name == "" || seen[tc.Name] || len(tc.Argv) == 0 || len(tc.Want) == 0 {
				t.Fatalf("%s: empty or duplicate case: %+v", path, tc)
			}
			seen[tc.Name] = true
			t.Run(strings.TrimSuffix(filepath.Base(path), ".json")+"/"+tc.Name, func(t *testing.T) {
				inv := model.Invocation{Argv: tc.Argv, Env: tc.Env, Aliases: tc.Aliases, Cwd: tc.Cwd}
				if tc.Stdin != nil {
					inv.Stdin = model.StdinData{Status: model.StdinCaptured, Data: []byte(*tc.Stdin)}
				}
				result := Normalize(inv)
				if len(result.Errors) != 0 {
					t.Fatalf("unexpected normalization errors: %v", result.Errors)
				}
				data, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				var got map[string]any
				if err := json.Unmarshal(data, &got); err != nil {
					t.Fatal(err)
				}
				if err := corpusSubset("result", got, tc.Want); err != nil {
					t.Fatalf("argv=%q env=%v: %v\nresult: %s", tc.Argv, tc.Env, err, data)
				}
				for _, op := range result.Operations {
					for _, set := range []*model.TargetSet{op.Target.Refs, op.Target.Paths, op.Target.Revisions, op.Target.ConfigKeys} {
						if set != nil && (set.Kind == model.SetExact || set.Kind == model.SetSet || set.Kind == model.SetPattern) && len(set.Values) == 0 {
							t.Fatalf("empty known target: %+v", op.Target)
						}
					}
					if op.Effect != model.EffectRead && (result.Classification.MutationFree || result.Classification.RetrySafe) {
						t.Fatal("write/unknown operation marked mutation-free or retry-safe")
					}
				}
			})
		}
	}
	if count < 150 {
		t.Fatalf("corpus has %d cases; need at least 150", count)
	}
}

// Objects are subsets; arrays have exact length and order, including operations.
func corpusSubset(path string, got, want any) error {
	switch want := want.(type) {
	case map[string]any:
		object, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: got %v, want object", path, got)
		}
		for key, value := range want {
			actual, exists := object[key]
			if !exists {
				return fmt.Errorf("%s.%s: missing", path, key)
			}
			if err := corpusSubset(path+"."+key, actual, value); err != nil {
				return err
			}
		}
	case []any:
		array, ok := got.([]any)
		if !ok || len(array) != len(want) {
			return fmt.Errorf("%s: got %v, want array of length %d", path, got, len(want))
		}
		for i, value := range want {
			if err := corpusSubset(fmt.Sprintf("%s[%d]", path, i), array[i], value); err != nil {
				return err
			}
		}
	default:
		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("%s: got %v, want %v", path, got, want)
		}
	}
	return nil
}

func TestCorpusSubset(t *testing.T) {
	for _, tc := range []struct {
		name      string
		got, want string
		match     bool
	}{
		{"subset", `{"a":{"b":true,"c":2}}`, `{"a":{"b":true}}`, true},
		{"value", `{"a":false}`, `{"a":true}`, false},
		{"missing", `{}`, `{"a":null}`, false},
		{"extra operation", `[1,2]`, `[1]`, false},
		{"order", `[1,2]`, `[2,1]`, false},
		{"array type", `{}`, `[]`, false},
		{"object type", `[]`, `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got, want any
			if err := json.Unmarshal([]byte(tc.got), &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if err := corpusSubset("result", got, want); (err == nil) != tc.match {
				t.Fatalf("match=%v, error=%v", tc.match, err)
			}
		})
	}
}
