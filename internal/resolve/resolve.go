// Package resolve fills targets from local Git metadata, without subprocesses or network.
package resolve

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mkusaka/gthok/internal/gitmeta"
	"github.com/mkusaka/gthok/internal/model"
)

type Resolver struct {
	Env     map[string]string
	Cwd     string
	RealGit string
	Timeout time.Duration
	mu      sync.Mutex
	local   *model.ResolvedContext
}

func (r *Resolver) Local() model.ResolvedContext {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.local == nil {
		env := r.Env
		if env == nil {
			env = make(map[string]string)
			for _, value := range os.Environ() {
				k, v, _ := strings.Cut(value, "=")
				env[k] = v
			}
		}
		cwd := r.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		c := model.ResolvedContext{Sources: make(map[string]string)}
		root, dir, common, bare, err := gitmeta.Repository(cwd, env)
		c.RepoRoot, c.GitDir, c.CommonDir, c.Bare = root, dir, common, bare
		if err != nil {
			c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "target.repoRoot", Reason: err.Error(), Source: "git"})
		}
		if dir != "" {
			c.Sources["gitDir"], c.Sources["commonDir"] = "git", "git"
			if root != "" {
				c.Sources["repoRoot"] = "git"
			}
			if data, err := gitmeta.ReadBounded(filepath.Join(dir, "HEAD")); err == nil {
				head := strings.TrimSpace(string(data))
				if strings.HasPrefix(head, "ref: ") {
					c.Branch = strings.TrimPrefix(head, "ref: ")
					c.HeadSHA, err = gitmeta.ReadRef(dir, common, c.Branch, 0)
					if err != nil {
						c.Unresolved = append(c.Unresolved, model.Unresolved{Field: "headSha", Reason: err.Error(), Source: "git"})
					}
				} else if gitmeta.ValidSHA(head) {
					c.HeadSHA = head
				}
			}
		}
		r.local = &c
	}
	c := *r.local
	c.Sources = make(map[string]string, len(r.local.Sources))
	for k, v := range r.local.Sources {
		c.Sources[k] = v
	}
	c.Unresolved = append([]model.Unresolved(nil), r.local.Unresolved...)
	return c
}

// Fill retains its original entrypoint; allowAPI never permits remote queries.
func (r *Resolver) Fill(ctx context.Context, res *model.Result, need []string, allowAPI bool) []string {
	if ctx.Err() != nil {
		return []string{ctx.Err().Error()}
	}
	c := r.Local()
	for i := range res.Operations {
		target := &res.Operations[i].Target
		if target.RepoRoot == "" {
			target.RepoRoot = c.RepoRoot
		}
		if target.GitDir == "" {
			target.GitDir = c.GitDir
		}
		if target.CommonDir == "" {
			target.CommonDir = c.CommonDir
		}
		if target.Worktree == "" {
			target.Worktree = c.RepoRoot
		}
	}
	return nil
}
