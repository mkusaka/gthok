package git

import (
	"strings"

	"github.com/mkusaka/gthok/internal/model"
)

func init() {
	register("status", status)
	register("version", readInfo)
	register("help", readInfo)
}

func readInfo(c *Context) []model.Operation {
	command := c.Parsed.Path[0]
	if c.Parsed.Help {
		command = "help"
	}
	return []model.Operation{{Family: "git." + command, Action: "git." + command + ".read",
		Effect: model.EffectRead, Coverage: model.CoverageSemantic}}
}

func status(c *Context) []model.Operation {
	op := model.Operation{Family: "status", Action: "status.show", Effect: model.EffectRead,
		Coverage: model.CoverageSemantic, Scopes: []model.Scope{model.ScopeIndex, model.ScopeWorktree}}
	locks := strings.ToLower(c.Invocation.Env["GIT_OPTIONAL_LOCKS"])
	optionalLocks := locks != "0" && locks != "false" && locks != "no" && locks != "off" && locks != ""
	if _, set := c.Invocation.Env["GIT_OPTIONAL_LOCKS"]; !set {
		optionalLocks = true
	}
	for i := 0; i < len(c.Global.Tokens); i++ {
		switch c.Global.Tokens[i] {
		case "--no-optional-locks":
			optionalLocks = false
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env", "--attr-source", "--shallow-file":
			i++
		}
	}
	if optionalLocks {
		op.Capabilities = append(op.Capabilities, model.CapabilityIndexRefresh)
	}
	if len(c.Parsed.Positionals) > 0 {
		op.Target.Paths = &model.TargetSet{Kind: model.SetPattern, Values: c.Parsed.Values(), Complete: true}
	}
	return []model.Operation{op}
}
