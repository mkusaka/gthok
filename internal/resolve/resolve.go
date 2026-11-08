// Package resolve fills targets using bounded, local-only queries to real Git.
package resolve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mkusaka/gthok/internal/gitmeta"
	"github.com/mkusaka/gthok/internal/model"
)

// Resolver belongs to one invocation. Configure it before use. Env is the
// complete child environment (nil snapshots os.Environ). RealGit must be the
// runtime-discovered executable, not the shim. Global contains original Git
// root options; Cwd/Env must not already have those options applied.
type Resolver struct {
	Env     map[string]string
	Cwd     string
	RealGit string
	Global  []string
	Timeout time.Duration

	mu          sync.Mutex
	local       *model.ResolvedContext
	env         map[string]string
	cwd         string
	dir         string
	globals     []string
	cache       map[query]queryResult
	config      map[string][]string
	configOrder []string
	refs        map[string]bool
	refsErr     error
}

func (r *Resolver) localLocked(ctx context.Context) *model.ResolvedContext {
	if r.local != nil {
		return r.local
	}
	c := &model.ResolvedContext{Sources: make(map[string]string), Remotes: make(map[string]model.RemoteInfo)}
	r.local, r.cache, r.env = c, make(map[query]queryResult), make(map[string]string)
	if r.Env == nil {
		for _, value := range os.Environ() {
			k, v, _ := strings.Cut(value, "=")
			r.env[k] = v
		}
	} else {
		for k, v := range r.Env {
			r.env[k] = v
		}
	}
	r.cwd = r.Cwd
	if r.cwd == "" {
		r.cwd, _ = os.Getwd()
	}
	var err error
	r.cwd, err = filepath.Abs(r.cwd)
	if err != nil {
		c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "target.repoRoot", Reason: "invocation directory is unavailable", Source: "env"})
		return c
	}
	r.globals, err = selectionGlobals(r.Global)
	if err != nil {
		c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "target.repoRoot", Reason: err.Error(), Source: "argv"})
		return c
	}
	r.dir = r.cwd
	if physical, err := filepath.EvalSymlinks(r.dir); err == nil {
		r.dir = physical
	}
	for i := 0; i < len(r.globals); i++ {
		key := r.globals[i]
		if key == "--bare" || strings.HasPrefix(key, "--config-env=") {
			continue
		}
		i++
		if key == "-C" && r.globals[i] != "" {
			r.dir = gitmeta.Absolute(r.dir, r.globals[i])
			if physical, err := filepath.EvalSymlinks(r.dir); err == nil {
				r.dir = physical
			}
		}
	}
	data, err := r.run(ctx, query{kind: metadataQuery})
	if err != nil {
		c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "target.repoRoot", Reason: err.Error(), Source: "git"})
		return c
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 5 || (lines[2] != "true" && lines[2] != "false") || (lines[3] != "true" && lines[3] != "false") {
		c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "target.gitDir", Reason: "invalid Git repository metadata", Source: "git"})
		return c
	}
	c.GitDir, c.CommonDir, c.Bare = gitmeta.Absolute(r.dir, lines[0]), gitmeta.Absolute(r.dir, lines[1]), lines[2] == "true"
	c.Sources["gitDir"], c.Sources["commonDir"], c.Sources["bare"] = "git", "git", "git"
	if !c.Bare {
		if data, err := r.run(ctx, query{kind: rootQuery}); err == nil {
			root := strings.TrimSuffix(string(data), "\n")
			if strings.ContainsRune(root, '\n') {
				c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "target.repoRoot", Reason: "repository path contains a line break", Source: "git"})
			} else {
				c.RepoRoot = root
				c.Sources["repoRoot"] = "git"
			}
		} else {
			c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "target.repoRoot", Reason: err.Error(), Source: "git"})
		}
	}
	if data, err := r.run(ctx, query{kind: headQuery}); err == nil {
		c.Branch = strings.TrimSpace(string(data))
		c.Sources["branch"] = "git"
	} else if !exitCode(err, 1) {
		c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "branch", Reason: err.Error(), Source: "git"})
	}
	if data, err := r.run(ctx, query{kind: commitQuery, value: "HEAD"}); err == nil && gitmeta.ValidSHA(strings.TrimSpace(string(data))) {
		c.HeadSHA = strings.TrimSpace(string(data))
		c.Sources["headSha"] = "git"
	} else {
		reason := "HEAD has no locally available commit"
		if err != nil {
			reason = err.Error()
		}
		c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "headSha", Reason: reason, Source: "git"})
	}
	data, err = r.run(ctx, query{kind: configQuery})
	if err != nil {
		c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "remotes", Reason: err.Error(), Source: "git"})
		return c
	}
	r.config = make(map[string][]string)
	for _, entry := range strings.Split(string(data), "\x00") {
		if entry == "" {
			continue
		}
		key, value, hasValue := strings.Cut(entry, "\n")
		if !hasValue {
			value = "true"
		}
		if _, exists := r.config[key]; !exists {
			r.configOrder = append(r.configOrder, key)
		}
		r.config[key] = append(r.config[key], value)
	}
	names := make(map[string]bool)
	for key := range r.config {
		if rest, ok := strings.CutPrefix(key, "remote."); ok {
			if dot := strings.LastIndex(rest, "."); dot > 0 {
				names[rest[:dot]] = true
			}
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		info := model.RemoteInfo{}
		unsafeURL := false
		for _, raw := range append(r.configURLs(name, false), r.configURLs(name, true)...) {
			unsafeURL = unsafeURL || strings.ContainsAny(raw, "\r\n")
		}
		if unsafeURL {
			c.Remotes[name] = info
			c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "remotes." + name, Reason: "remote URL contains a line break", Source: "git"})
			continue
		}
		for _, kind := range []queryKind{remoteQuery, pushURLQuery} {
			data, err := r.run(ctx, query{kind: kind, value: name})
			urls := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if err != nil {
				// get-url rejects remotes defined only in global/-c config.
				urls = r.configURLs(name, kind == pushURLQuery)
				if !exitCode(err, 2) || len(urls) == 0 {
					c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "remotes." + name, Reason: err.Error(), Source: "git"})
					continue
				}
			}
			for _, raw := range urls {
				if raw == "" {
					continue
				}
				if kind == remoteQuery && info.URL == "" {
					info.URL = cleanURL(raw)
				} else if kind == pushURLQuery {
					info.PushURLs = append(info.PushURLs, cleanURL(raw))
				}
			}
		}
		c.Remotes[name] = info
	}
	c.Sources["remotes"] = "git"
	return c
}

// Local caches local queries for this invocation and returns an independent copy.
func (r *Resolver) Local() model.ResolvedContext {
	r.mu.Lock()
	defer r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout())
	defer cancel()
	c := *r.localLocked(ctx)
	c.Sources = make(map[string]string, len(r.local.Sources))
	for k, v := range r.local.Sources {
		c.Sources[k] = v
	}
	c.Remotes = make(map[string]model.RemoteInfo, len(r.local.Remotes))
	for k, v := range r.local.Remotes {
		v.PushURLs = append([]string(nil), v.PushURLs...)
		c.Remotes[k] = v
	}
	c.Unresolved = append([]model.Unresolved(nil), r.local.Unresolved...)
	return c
}

// Fill retains the ghok entrypoint; allowAPI never permits remote queries.
// All locally resolvable target fields are filled, regardless of need.
func (r *Resolver) Fill(ctx context.Context, res *model.Result, need []string, allowAPI bool) []string {
	if err := ctx.Err(); err != nil {
		return []string{err.Error()}
	}
	if res == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	c := r.localLocked(ctx)
	var operations []model.Operation
	for _, original := range res.Operations {
		if original.Execution == model.ExecutionSkipped || original.Execution == model.ExecutionNotSelected {
			operations = append(operations, original)
			continue
		}
		for _, op := range r.expandGroup(original) {
			t := &op.Target
			if t.RepoRoot == "" {
				t.RepoRoot = c.RepoRoot
			}
			if t.GitDir == "" {
				t.GitDir = c.GitDir
			}
			if t.CommonDir == "" {
				t.CommonDir = c.CommonDir
			}
			if t.Worktree == "" {
				t.Worktree = c.RepoRoot
			}
			for _, u := range c.Unresolved {
				if strings.HasPrefix(u.Field, "target.") && !targetKnown(*t, u.Field) {
					setUnresolved(&op, u.Field, u.Reason, u.Source)
				}
			}
			if c.GitDir == "" {
				t.Remote, t.RemoteURL, t.PushURL = cleanURL(t.Remote), cleanURL(t.RemoteURL), cleanURL(t.PushURL)
				if t.Refs != nil {
					t.Refs.Complete = false
					setUnresolved(&op, "target.refs", "repository metadata is unavailable", "git")
				}
				operations = append(operations, op)
				continue
			}
			if isPush(op) && t.Remote == "" {
				t.Remote = r.defaultRemote(true)
			} else if t.Remote == "" && (op.Family == "remote.fetch" || op.Family == "remote.pull") {
				t.Remote = r.defaultRemote(false)
			}
			if isPush(op) {
				r.pushRefs(ctx, &op)
			} else if localRefs(op) {
				r.localRefs(ctx, &op)
			}
			for _, field := range need {
				if strings.HasPrefix(field, "target.") && !targetKnown(*t, field) && !hasUnresolved(op, field) {
					setUnresolved(&op, field, "requested field could not be resolved locally", "git")
				}
			}
			clearKnown(&op)
			operations = append(operations, r.remoteTargets(ctx, op)...)
		}
	}
	res.Operations = operations
	updateResolution(res)
	if err := ctx.Err(); err != nil {
		return []string{err.Error()}
	}
	return nil
}

func setUnresolved(op *model.Operation, field, reason, source string) {
	for i := range op.Unresolved {
		if op.Unresolved[i].Field == field {
			op.Unresolved[i] = model.Unresolved{Field: field, Reason: reason, Source: source}
			return
		}
	}
	op.Unresolved = append(op.Unresolved, model.Unresolved{Field: field, Reason: reason, Source: source})
}

func hasUnresolved(op model.Operation, field string) bool {
	for _, u := range op.Unresolved {
		if u.Field == field {
			return true
		}
	}
	return false
}

func targetKnown(t model.Target, field string) bool {
	switch field {
	case "target.repoRoot":
		return t.RepoRoot != ""
	case "target.gitDir":
		return t.GitDir != ""
	case "target.commonDir":
		return t.CommonDir != ""
	case "target.worktree":
		return t.Worktree != ""
	case "target.remote":
		return t.Remote != ""
	case "target.remoteUrl":
		return t.RemoteURL != ""
	case "target.pushUrl":
		return t.PushURL != ""
	case "target.refs":
		return t.Refs != nil && t.Refs.Complete && t.Refs.Kind != model.SetUnknown
	}
	return false
}

func clearKnown(op *model.Operation) {
	remaining := op.Unresolved[:0]
	for _, u := range op.Unresolved {
		if u.Field == "target.remote" || !targetKnown(op.Target, u.Field) || (u.Source != "git" && u.Source != "resolver") {
			remaining = append(remaining, u)
		}
	}
	op.Unresolved = remaining
}

func cloneOperation(op model.Operation) model.Operation {
	op.Unresolved = append([]model.Unresolved(nil), op.Unresolved...)
	if op.Target.Refs != nil {
		refs := *op.Target.Refs
		refs.Values = append([]string(nil), refs.Values...)
		op.Target.Refs = &refs
	}
	return op
}

func updateResolution(res *model.Result) {
	unresolved, known, unknown := false, false, false
	for _, op := range res.Operations {
		if op.Execution == model.ExecutionSkipped || op.Execution == model.ExecutionNotSelected {
			continue
		}
		unresolved = unresolved || len(op.Unresolved) > 0 || (op.Target.Refs != nil && !op.Target.Refs.Complete)
		unknown = unknown || op.Family == model.FamilyUnknown || op.Effect == model.EffectUnknown || op.UnknownReason != model.UnknownNone
		known = known || op.Target.GitDir != ""
	}
	switch {
	case unknown:
		res.Classification.Resolution = model.ResolutionUnresolved
	case !unresolved:
		res.Classification.Resolution = model.ResolutionComplete
	case known:
		res.Classification.Resolution = model.ResolutionPartial
	default:
		res.Classification.Resolution = model.ResolutionUnresolved
	}
}

func (r *Resolver) last(key string) string {
	values := r.config[key]
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func (r *Resolver) defaultRemote(push bool) string {
	branch := strings.TrimPrefix(r.local.Branch, "refs/heads/")
	if push {
		if value := r.last("branch." + branch + ".pushremote"); value != "" {
			return value
		}
		if value := r.last("remote.pushdefault"); value != "" {
			return value
		}
	}
	if value := r.last("branch." + branch + ".remote"); value != "" {
		return value
	}
	if len(r.local.Remotes) == 1 {
		for name := range r.local.Remotes {
			return name
		}
	}
	if _, ok := r.local.Remotes["origin"]; ok {
		return "origin"
	}
	return ""
}

func unresolvedRefs(op *model.Operation, reason, source string) {
	op.Target.Refs = model.UnknownSet()
	setUnresolved(op, "target.refs", reason, source)
}

func requireRemote(op *model.Operation) {
	if op.Target.Remote == "" {
		setUnresolved(op, "target.remote", "no configured default remote", "git")
	}
}

func (r *Resolver) loadRefs(ctx context.Context) error {
	if r.refs != nil || r.refsErr != nil {
		return r.refsErr
	}
	data, err := r.run(ctx, query{kind: refsQuery})
	if err != nil {
		r.refsErr = err
		return err
	}
	r.refs = make(map[string]bool)
	for _, name := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if name != "" {
			r.refs[name] = true
		}
	}
	return nil
}

func (r *Resolver) dwim(ctx context.Context, name string) (string, error) {
	if name == "HEAD" && r.local.Branch != "" {
		return r.local.Branch, nil
	}
	if err := r.loadRefs(ctx); err != nil {
		return "", err
	}
	candidates := []string{name, "refs/" + name, "refs/tags/" + name, "refs/heads/" + name, "refs/remotes/" + name, "refs/remotes/" + name + "/HEAD"}
	found := ""
	seen := make(map[string]bool)
	for _, ref := range candidates {
		if r.refs[ref] && !seen[ref] {
			if found != "" {
				return "", fmt.Errorf("local ref name is ambiguous")
			}
			found, seen[ref] = ref, true
		}
	}
	if found == "" {
		return "", fmt.Errorf("ref has no unambiguous local full name")
	}
	return found, nil
}

func localRefs(op model.Operation) bool {
	for _, scope := range op.Scopes {
		if scope == model.ScopeLocalRef {
			return true
		}
	}
	return false
}

func (r *Resolver) localRefs(ctx context.Context, op *model.Operation) {
	refs := op.Target.Refs
	if refs == nil || (refs.Kind != model.SetExact && refs.Kind != model.SetSet) {
		return
	}
	for i, name := range refs.Values {
		if strings.HasPrefix(name, "refs/") {
			continue
		}
		full, err := r.dwim(ctx, name)
		if err != nil {
			unresolvedRefs(op, err.Error(), "git")
			return
		}
		refs.Values[i] = full
	}
	refs.Complete = len(refs.Values) > 0
}
