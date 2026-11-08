package schema

import (
	"github.com/mkusaka/gthok/internal/delegates"
	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/policy"
	"github.com/mkusaka/gthok/internal/protocol"
)

// Management contracts mirror the CLI envelopes; their nested types are shared
// with the runtime. Keep CLI-shape tests alongside schema generation tests.
type NormalizeResult struct {
	model.Result
	Delegate *delegates.Selection `json:"delegate,omitempty"`
}

type Transformed struct {
	Argv       []string          `json:"argv"`
	Transforms []string          `json:"transforms"`
	Operations []model.Operation `json:"operations"`
	Env        []string          `json:"env,omitempty"`
	EnvBy      map[string]string `json:"envBy,omitempty"`
	EnvUnset   []string          `json:"envUnset,omitempty"`
}

type Explain struct {
	Normalization    model.Result           `json:"normalization"`
	Context          *model.ResolvedContext `json:"context,omitempty"`
	Policy           policy.Evaluation      `json:"policy"`
	Hooks            map[string][]string    `json:"hooks"`
	Delegate         *delegates.Selection   `json:"delegate,omitempty"`
	Warnings         []protocol.Warning     `json:"warnings,omitempty"`
	Transformed      *Transformed           `json:"transformed,omitempty"`
	TransformResults []protocol.HookResult  `json:"transformResults,omitempty"`
	Notes            []string               `json:"notes,omitempty"`
	Diagnostics      []string               `json:"diagnostics,omitempty"`
}

type HooksTest struct {
	Phase     string                `json:"phase" enum:"pre,post"`
	Synthetic bool                  `json:"synthetic"`
	Results   []protocol.HookResult `json:"results"`
	Aggregate struct {
		Decision protocol.Decision `json:"decision" enum:"allow,deny,abstain"`
		Message  string            `json:"message,omitempty"`
	} `json:"aggregate"`
	Policy policy.Evaluation `json:"policy"`
	Final  struct {
		Decision  protocol.Decision `json:"decision" enum:"allow,deny,abstain,audit"`
		WouldDeny bool              `json:"wouldDeny"`
	} `json:"final"`
	Transformed      *Transformed          `json:"transformed,omitempty"`
	TransformResults []protocol.HookResult `json:"transformResults,omitempty"`
	Warnings         []protocol.Warning    `json:"warnings,omitempty"`
}

type ActionFamily struct {
	Family  string        `json:"family"`
	Actions []ActionEntry `json:"actions"`
}

type ActionEntry struct {
	Action string        `json:"action"`
	Effect model.Effect  `json:"effect" enum:"read,write,unknown"`
	Scopes []model.Scope `json:"scopes"`
	Git    []string      `json:"git"`
}

// Version describes release metadata. The current version command displays it
// as text, not JSON; this contract is for consumers storing that metadata.
type Version struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}
