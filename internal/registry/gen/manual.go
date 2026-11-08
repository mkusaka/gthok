package main

import (
	"fmt"
	"strings"

	"github.com/mkusaka/gthok/internal/registry"
)

// Manual parsers remain adapters. This extracts literal comparisons and their
// argument consumption, not help/completion text, for shared adapter grammars.
func manualOptions(f *sourceFile, names ...string) ([]registry.Option, error) {
	type variants struct {
		option                   registry.Option
		bare, attached, required bool
	}
	options := map[string]variants{}
	for _, name := range names {
		fn, ok := f.functions[name]
		if !ok {
			return nil, fmt.Errorf("%s: missing %s", f.name, name)
		}
		ts := fn.body
		for i := 0; i+1 < len(ts); i++ {
			kind := ts[i].text
			if kind != "strcmp" && kind != "skip_prefix" && kind != "starts_with" && kind != "parse_long_opt" {
				continue
			}
			if ts[i+1].text != "(" {
				continue
			}
			end, err := matching(ts, i+1)
			if err != nil {
				return nil, err
			}
			args, err := split(ts[i+2 : end])
			if err != nil {
				return nil, err
			}
			if len(args) < 2 {
				continue
			}
			idx := 1
			if kind == "parse_long_opt" {
				idx = 0
			}
			s := literal(args[idx])
			if kind == "strcmp" && s == "" {
				s = literal(args[0])
			}
			if kind == "parse_long_opt" {
				s = "--" + s
			}
			if !strings.HasPrefix(s, "-") || s == "--" || s == "-" {
				continue
			}
			var branch []token
			for j := end + 1; j < len(ts); j++ {
				if ts[j].text == "{" {
					close, err := matching(ts, j)
					if err != nil {
						return nil, err
					}
					branch = ts[j+1 : close]
					break
				}
				if ts[j].text == ";" {
					branch = ts[end+1 : j]
					break
				}
			}
			attached := strings.HasSuffix(s, "=") || kind == "skip_prefix" && !strings.HasPrefix(s, "--")
			body := text(branch)
			required := kind == "parse_long_opt" || !attached && (strings.Contains(body, "argv[1]") || strings.Contains(body, "(*argv)[1]"))
			s = strings.TrimSuffix(s, "=")
			v, exists := options[s]
			if !exists {
				v.option = registry.Option{Kind: "manual:" + kind, Type: "manual", Negatable: false, Source: registry.Source{File: f.name, Line: ts[i].line, Entrypoint: name}}
				if strings.HasPrefix(s, "--") {
					v.option.Long = s[2:]
				} else if len(s) == 2 {
					v.option.Short = s[1:]
				} else {
					continue
				}
			}
			v.attached = v.attached || attached
			v.bare = v.bare || !attached
			v.required = v.required || required
			options[s] = v
		}
	}
	var out []registry.Option
	for _, key := range sortedKeys(options) {
		v := options[key]
		v.option.Arity = "none"
		if v.required || v.attached && !v.bare {
			v.option.Arity = "required"
		} else if v.attached {
			v.option.Arity = "optional"
		}
		if v.attached {
			v.option.Flags = []string{"ATTACHED_VALUE"}
		}
		if key == "--exec-path" {
			v.option.Arity = "optional"
			v.option.Flags = []string{"ATTACHED_VALUE"}
		}
		out = append(out, v.option)
	}
	return out, nil
}

// These entrypoints consume argv outside parse-options tables.
func (e *extractor) manualGrammars() error {
	fetch := e.grammars["fetch-pack"]
	options, err := manualOptions(e.files["builtin/fetch-pack.c"], "cmd_fetch_pack")
	if err != nil {
		return err
	}
	fetch.Options = options
	fetch.ParseFlags = []string{"PARSE_OPT_STOP_AT_NON_OPTION"}
	e.grammars["fetch-pack"] = fetch

	// Both 'reflog' and 'reflog show' delegate their arguments to cmd_log_reflog.
	reflog := e.grammars["reflog"]
	reflog.Parser = "revision"
	var logOptions []registry.Option
	for _, option := range e.grammars["log"].Options {
		if option.Short != "L" { // Line-range logging is incompatible with reflog walks.
			logOptions = append(logOptions, option)
		}
	}
	// The top-level completion helper does not advertise delegated log flags.
	for i := range logOptions {
		logOptions[i].Flags = append(append([]string(nil), logOptions[i].Flags...), "PARSE_OPT_NOCOMPLETE")
	}
	reflog.Options = append(reflog.Options, logOptions...)
	for i := range reflog.Subcommands {
		if reflog.Subcommands[i].Name == "show" {
			reflog.Subcommands[i].Options = append(reflog.Subcommands[i].Options, logOptions...)
			reflog.Subcommands[i].Parser = "revision"
		}
	}
	e.grammars["reflog"] = reflog

	bisect := e.grammars["bisect"]
	f := e.files["builtin/bisect.c"]
	start, err := manualOptions(f, "bisect_start")
	if err != nil {
		return err
	}
	for i := range start {
		switch start[i].Long {
		case "term-good", "term-old", "term-bad", "term-new":
			start[i].Arity, start[i].Flags = "required", nil
		}
	}
	for i := range bisect.Subcommands {
		sub := &bisect.Subcommands[i]
		switch sub.Name {
		case "start":
			sub.Options = start
		case "terms":
			for _, name := range []string{"term-good", "term-old", "term-bad", "term-new"} {
				sub.Options = append(sub.Options, registry.Option{Long: name, Arity: "none", Type: "manual", Kind: "manual:one_of", Source: registry.Source{File: f.name, Entrypoint: "bisect_terms", Line: f.functions["bisect_terms"].line}})
			}
		case "run":
			// The executable and every following token belong to the child.
			sub.ParseFlags = []string{"PARSE_OPT_STOP_AT_NON_OPTION", "PARSE_OPT_NO_INTERNAL_HELP"}
		}
	}
	e.grammars["bisect"] = bisect
	return nil
}
