package git

import (
	"github.com/mkusaka/gthok/internal/gitargs"
	"github.com/mkusaka/gthok/internal/model"
)

// NeedsStdin only requests finite, one-way input used by analysis. Interactive
// bidirectional protocols are intentionally never read ahead by the runtime.
func NeedsStdin(argv []string) (bool, string) {
	g, err := gitargs.Parse(argv)
	if err != nil {
		return false, ""
	}
	p, err := parseCommand(g)
	if err != nil || len(p.Unknown) > 0 {
		return false, ""
	}
	for _, o := range p.Options {
		if o.Negated {
			continue
		}
		if o.Name == "pathspec-from-file" && o.Value == "-" {
			return true, "pathspecs"
		}
		if o.Name == "file" && o.Value == "-" {
			switch g.Command {
			case "commit", "tag", "merge":
				return true, g.Command + "-message"
			case "notes":
				return true, "notes-message"
			}
		}
	}
	if commands[g.Command].Parser == "revision" && p.Has("stdin") {
		return true, "revisions"
	}
	switch g.Command {
	case "hash-object":
		if p.Has("stdin") || p.Has("stdin-paths") {
			return true, "hash-object"
		}
	case "update-index":
		if p.Has("stdin") || p.Has("index-info") {
			return true, "index-paths"
		}
	}
	return false, ""
}

func inputProtocol(c *Context) bool {
	if c.Invocation.Stdin.Status == model.StdinCaptured {
		return false
	}
	if commands[c.Parsed.Path[0]].Parser == "revision" && c.Parsed.Has("stdin") {
		return true
	}
	switch c.Parsed.Path[0] {
	case "update-ref":
		return c.Parsed.Has("stdin")
	case "cat-file":
		return c.Parsed.Has("batch") || c.Parsed.Has("batch-check") || c.Parsed.Has("batch-command")
	case "fast-import", "credential":
		return true
	}
	return false
}
