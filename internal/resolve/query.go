package resolve

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mkusaka/gthok/internal/gitmeta"
)

// No caller can supply a command or an option to a query.
type queryKind uint8

const (
	metadataQuery queryKind = iota
	rootQuery
	commitQuery
	symbolicQuery
	headQuery
	configQuery
	configGetQuery
	configAllQuery
	configRegexpQuery
	refsQuery
	showRefsQuery
	remoteQuery
	pushURLQuery
	filesQuery
	trackedFileQuery
	objectTypeQuery
	objectSizeQuery
)

type query struct {
	kind  queryKind
	value string
}

func (q query) args() ([]string, error) {
	fileQuery := q.kind == filesQuery || q.kind == trackedFileQuery
	if strings.ContainsRune(q.value, '\x00') || (!fileQuery && (strings.ContainsAny(q.value, "\r\n") || strings.HasPrefix(q.value, "-"))) {
		return nil, fmt.Errorf("invalid resolver query operand")
	}
	switch q.kind {
	case metadataQuery:
		// --show-toplevel fails in bare repositories, so it is a separate query.
		return []string{"rev-parse", "--git-dir", "--git-common-dir", "--is-bare-repository", "--is-inside-work-tree", "--show-prefix"}, nil
	case rootQuery:
		return []string{"rev-parse", "--show-toplevel"}, nil
	case commitQuery:
		if q.value != "" {
			return []string{"rev-parse", "--verify", "--quiet", "--end-of-options", q.value + "^{commit}"}, nil
		}
	case symbolicQuery:
		if q.value != "" {
			return []string{"rev-parse", "--symbolic-full-name", q.value}, nil
		}
	case headQuery:
		return []string{"symbolic-ref", "-q", "HEAD"}, nil
	case configQuery:
		return []string{"config", "--null", "--list"}, nil
	case configGetQuery, configAllQuery, configRegexpQuery:
		if q.value != "" {
			option := map[queryKind]string{configGetQuery: "--get", configAllQuery: "--get-all", configRegexpQuery: "--get-regexp"}[q.kind]
			return []string{"config", "--null", option, q.value}, nil
		}
	case refsQuery:
		args := []string{"for-each-ref", "--format=%(refname)"}
		if q.value != "" {
			args = append(args, q.value)
		}
		return args, nil
	case showRefsQuery:
		return []string{"show-ref"}, nil
	case remoteQuery, pushURLQuery:
		if q.value != "" {
			args := []string{"remote", "get-url", "--all"}
			if q.kind == pushURLQuery {
				args = append(args, "--push")
			}
			return append(args, q.value), nil
		}
	case filesQuery, trackedFileQuery:
		args := []string{"ls-files", "-z"}
		if q.kind == trackedFileQuery {
			args = append(args, "--error-unmatch")
		}
		return append(args, "--", q.value), nil
	case objectTypeQuery, objectSizeQuery:
		if gitmeta.ValidSHA(q.value) {
			option := "-t"
			if q.kind == objectSizeQuery {
				option = "-s"
			}
			return []string{"cat-file", option, q.value}, nil
		}
	}
	return nil, fmt.Errorf("invalid resolver query")
}

// selectionGlobals keeps only repository/config options, never --exec-path or
// options that could change a query's output. Values are separate argv entries.
func selectionGlobals(tokens []string) ([]string, error) {
	var out []string
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		key, value, attached := strings.Cut(token, "=")
		if len(token) > 2 && (strings.HasPrefix(token, "-C") || strings.HasPrefix(token, "-c")) {
			key, value, attached = token[:2], token[2:], true
		}
		switch key {
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env":
			if !attached {
				i++
				if i == len(tokens) {
					return nil, fmt.Errorf("missing resolver global option value")
				}
				value = tokens[i]
			}
			if strings.ContainsRune(value, '\x00') {
				return nil, fmt.Errorf("invalid resolver global option value")
			}
			if key == "--config-env" {
				out = append(out, key+"="+value)
			} else {
				out = append(out, key, value)
			}
		case "--bare":
			if attached {
				return nil, fmt.Errorf("invalid resolver global option")
			}
			out = append(out, key)
		case "-p", "--paginate", "-P", "--no-pager", "--no-replace-objects", "--no-lazy-fetch", "--no-optional-locks", "--literal-pathspecs", "--glob-pathspecs", "--noglob-pathspecs", "--icase-pathspecs", "--no-advice":
			// These are not repository-selection or configuration options.
		default:
			return nil, fmt.Errorf("unsupported resolver global option")
		}
	}
	return out, nil
}

const maxQueryBytes = 1 << 20

type boundedOutput struct{ data bytes.Buffer }

func (b *boundedOutput) Len() int      { return b.data.Len() }
func (b *boundedOutput) Bytes() []byte { return b.data.Bytes() }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > maxQueryBytes-b.Len() {
		return 0, fmt.Errorf("resolver output exceeds 1 MiB")
	}
	return b.data.Write(p)
}

type queryResult struct {
	data []byte
	err  error
}

func (r *Resolver) run(ctx context.Context, q query) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if result, ok := r.cache[q]; ok {
		return result.data, result.err
	}
	args, err := q.args()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(r.RealGit) {
		return nil, fmt.Errorf("resolver requires an absolute real git path")
	}
	args = append(append([]string(nil), r.globals...), args...)
	callCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	cmd := exec.CommandContext(callCtx, r.RealGit, args...)
	cmd.Dir = r.cwd
	for k, v := range r.env {
		switch k {
		case "GIT_TERMINAL_PROMPT", "GIT_OPTIONAL_LOCKS", "GIT_PAGER", "PAGER", "LC_ALL", "GIT_NO_LAZY_FETCH", "GTHOK_RESOLVER_QUERY":
			continue
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	// Query provenance is for diagnostics/recorders, never an execution bypass.
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat", "PAGER=cat", "LC_ALL=C", "GIT_NO_LAZY_FETCH=1", "GTHOK_RESOLVER_QUERY=1")
	cmd.WaitDelay = 100 * time.Millisecond
	var stdout, stderr boundedOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if callCtx.Err() != nil {
		err = callCtx.Err()
	}
	if err != nil {
		// Neither argv nor Git's stderr is safe: either can contain credentials.
		err = fmt.Errorf("local git query %d failed: %w", q.kind, err)
	}
	result := queryResult{data: stdout.Bytes(), err: err}
	if callCtx.Err() == nil {
		r.cache[q] = result
	}
	return result.data, result.err
}

func (r *Resolver) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return 2 * time.Second
}
