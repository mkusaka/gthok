package redact

import (
	"slices"
	"strings"
)

// Argv returns an independent redacted copy, including Git's inline config.
func (r *Redactor) Argv(argv []string) []string {
	out := slices.Clone(argv)
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		out[i] = r.String(arg)
		if arg == "--" {
			for j := i + 1; j < len(argv); j++ {
				out[j] = r.String(argv[j])
			}
			break
		}
		if arg == "-c" && i+1 < len(argv) {
			i++
			out[i] = r.config(argv[i])
			continue
		}
		if strings.HasPrefix(arg, "-c") && len(arg) > 2 {
			out[i] = "-c" + r.config(arg[2:])
			continue
		}
		if strings.HasPrefix(arg, "--") {
			name, value, attached := strings.Cut(arg[2:], "=")
			if !sensitiveName(name) {
				continue
			}
			if attached {
				out[i] = r.String("--" + name + "=" + r.mask(value))
			} else if i+1 < len(argv) && argv[i+1] != "--" {
				i++
				out[i] = r.mask(argv[i])
			}
		}
	}
	return out
}

func (r *Redactor) config(value string) string {
	key, val, ok := strings.Cut(value, "=")
	if ok && (sensitivePath(key) || strings.HasSuffix(strings.ToLower(key), ".extraheader")) {
		return r.String(key + "=" + r.mask(val))
	}
	return r.String(value)
}
