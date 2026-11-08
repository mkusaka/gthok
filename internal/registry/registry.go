// Package registry exposes the grammar extracted from the pinned Git sources.
// Runtime use requires neither the sources nor a C compiler.
package registry

import (
	"embed"
	"encoding/json"
)

//go:generate go run ./gen -git-src $HOME/src/github.com/git/git -out data -verify

//go:embed data/*.json
var data embed.FS

type Source struct {
	File       string `json:"file"`
	Line       int    `json:"line"`
	Entrypoint string `json:"entrypoint,omitempty"`
}

type Option struct {
	Long      string   `json:"long,omitempty"`
	Short     string   `json:"short,omitempty"`
	Kind      string   `json:"kind"`
	Type      string   `json:"type"`
	Arity     string   `json:"arity"` // none, required, optional; LASTARG_DEFAULT remains in Flags.
	Negatable bool     `json:"negatable"`
	Flags     []string `json:"flags,omitempty"`
	Alias     string   `json:"alias,omitempty"`
	Callback  string   `json:"callback,omitempty"`
	Value     string   `json:"value,omitempty"`
	Source    Source   `json:"source"`
}

type Subcommand struct {
	Name       string   `json:"name"`
	Entrypoint string   `json:"entrypoint"`
	Source     Source   `json:"source"`
	Flags      []string `json:"flags,omitempty"`
	Grammar
}

// Parser identifies the adapter required in addition to any extracted options.
// A manual adapter is not an assertion that its Options are exhaustive.
type Grammar struct {
	Parser      string       `json:"parser"`
	Options     []Option     `json:"options,omitempty"`
	Subcommands []Subcommand `json:"subcommands,omitempty"`
	Tables      []string     `json:"tables,omitempty"`
	ParseFlags  []string     `json:"parseFlags,omitempty"`
	Exclusive   [][]string   `json:"exclusive,omitempty"`
	Adapters    []string     `json:"adapters,omitempty"`
}

type Command struct {
	Name           string   `json:"name"`
	Builtin        bool     `json:"builtin"`
	Implementation string   `json:"implementation"` // builtin, script, external, document, option-only
	Categories     []string `json:"categories,omitempty"`
	Flags          []string `json:"flags,omitempty"`
	Entrypoint     string   `json:"entrypoint,omitempty"`
	Source         Source   `json:"source"`
	Grammar        `json:"-"`
}

type Coverage struct {
	Commands int `json:"commands"`
	Builtins int `json:"builtins"`
	Tables   int `json:"tables"`
	Options  int `json:"options"`
	Adapters int `json:"adapters"`
}

type Exclusion struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type CompletionCheck struct {
	Command string   `json:"command"`
	Missing []string `json:"missing,omitempty"`
	Extra   []string `json:"extra,omitempty"`
}

type Verification struct {
	Git         string            `json:"git,omitempty"`
	Checked     int               `json:"checked"`
	Unsupported []string          `json:"unsupported,omitempty"`
	Mismatches  []CompletionCheck `json:"mismatches,omitempty"`
}

// ManifestInfo is the manifest type; Go cannot also name it Manifest because
// that identifier is used by the public Manifest function.
type ManifestInfo struct {
	Git              string        `json:"git"`
	Tag              string        `json:"tag"`
	Commit           string        `json:"commit"`
	GeneratorVersion string        `json:"generatorVersion"`
	Coverage         Coverage      `json:"coverage"`
	Exclusions       []Exclusion   `json:"exclusions"`
	Verification     *Verification `json:"verification,omitempty"`
}

func decode[T any](name string) T {
	b, err := data.ReadFile("data/" + name + ".json")
	if err != nil {
		panic(err)
	}
	var value T
	if err := json.Unmarshal(b, &value); err != nil {
		panic(err)
	}
	return value
}

// Commands returns a caller-owned command inventory including its grammar.
func Commands() map[string]Command {
	commands := decode[map[string]Command]("commands")
	grammars := decode[map[string]Grammar]("grammar")
	for name, command := range commands {
		command.Grammar = grammars[name]
		commands[name] = command
	}
	return commands
}

func Lookup(name string) (Command, bool) {
	command, ok := Commands()[name]
	return command, ok
}

func Manifest() ManifestInfo    { return decode[ManifestInfo]("manifest") }
func RevisionOptions() []Option { return decode[map[string]Grammar]("grammar")["$revision"].Options }
func DiffOptions() []Option     { return decode[map[string]Grammar]("grammar")["$diff"].Options }
func GlobalOptions() []Option   { return decode[map[string]Grammar]("grammar")["$global"].Options }

// SourceMap returns command paths and option spellings mapped to Git sources.
func SourceMap() map[string]Source { return decode[map[string]Source]("source-map") }
