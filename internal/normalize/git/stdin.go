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
	if err != nil || p.Help || len(p.Unknown) > 0 {
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
			case "commit", "tag":
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
	case "am", "apply":
		if g.Command == "am" && whLastMode(p, "start", "continue", "resolved", "abort", "skip", "quit", "retry", "show-current-patch", "allow-empty") != "start" {
			return false, ""
		}
		if len(p.Positionals) == 0 {
			return true, "patch"
		}
		for _, arg := range p.Positionals {
			if arg.Value == "-" {
				return true, "patch"
			}
		}
	case "fetch":
		if p.Has("stdin") {
			return true, "refspecs"
		}
	case "notes":
		if (p.Has("stdin") || p.Has("for-rewrite")) && len(p.Path) > 1 && (p.Path[1] == "copy" || p.Path[1] == "remove") {
			return true, "notes"
		}
	case "sparse-checkout":
		if len(p.Path) > 1 && (p.Path[1] == "check-rules" || p.Has("stdin") && (p.Path[1] == "set" || p.Path[1] == "add")) {
			return true, "sparse-patterns"
		}
	case "hash-object":
		if p.Has("stdin") {
			return true, "hash-object"
		}
	case "update-index":
		if p.Has("stdin") || p.Has("index-info") {
			return true, "index-paths"
		}
	}
	return false, ""
}

// StdinProtocol identifies streams that must stay live unless fstat proves
// stdin is a regular file. NeedsStdin deliberately excludes these protocols.
func StdinProtocol(argv []string) bool {
	g, err := gitargs.Parse(argv)
	if err != nil {
		return false
	}
	p, err := parseCommand(g)
	if err != nil || p.Help {
		return false
	}
	switch g.Command {
	case "update-ref", "send-pack", "check-ignore", "check-attr":
		return p.Has("stdin")
	case "cat-file":
		return !p.Has("batch-all-objects") && (p.Has("batch") || p.Has("batch-check") || p.Has("batch-command"))
	case "fast-import", "credential", "credential-cache", "credential-store", "credential-cache--daemon":
		return true
	case "hash-object":
		return p.Has("stdin-paths")
	}
	return false
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
