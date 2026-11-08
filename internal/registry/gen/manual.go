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
