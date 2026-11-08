package model

// Invocation is everything a normalizer may look at. Normalizers are pure:
// they must not touch the filesystem, network, environment or stdin except
// through the fields below.
type Invocation struct {
	// Argv are the arguments after the executable name (os.Args[1:]).
	Argv []string
	// Env is the process environment (KEY -> value).
	Env map[string]string
	// Cwd is the invocation directory.
	Cwd string
	// Stdin is what the wrapper captured from stdin, if anything.
	Stdin StdinData
	// ReadFile reads a file for analysis (e.g. commit -F file).
	// It is bounded by the caller (size limit). nil means files are unreadable;
	// normalizers must then report UnknownInputUnavailable, never guess.
	ReadFile func(path string) ([]byte, error)
	// Aliases are git aliases (alias.<name> / alias.<name>.command) for the
	// effective repository. Expansions starting with "!" are shell aliases.
	// nil means aliases were not loaded; non-builtin commands stay unknown.
	Aliases map[string]string
	// TemporaryFiles are private rewritten transports removed after execution.
	TemporaryFiles []string
}

// StdinStatus describes whether stdin bytes are available to normalizers.
type StdinStatus string

const (
	StdinNotCaptured StdinStatus = "not-captured" // not needed or not attempted
	StdinCaptured    StdinStatus = "captured"     // Data holds all stdin bytes (byte-exact)
	StdinTTY         StdinStatus = "tty"          // stdin is a terminal; deliberately not read
	StdinTooLarge    StdinStatus = "too-large"    // exceeded analysis limit; Data is nil
	StdinTimeout     StdinStatus = "timeout"      // producer did not finish in time; Data is nil
	StdinError       StdinStatus = "error"        // read error; see Error
)

type StdinData struct {
	Status StdinStatus
	Data   []byte
	Error  string
}

// ResolvedContext is what the local resolver learned. Every field is
// optional; Sources says where each known value came from ("argv", "env",
// "git") and Unresolved lists what could not be found. The resolver never
// uses the network.
type ResolvedContext struct {
	RepoRoot  string `json:"repoRoot,omitempty"`
	GitDir    string `json:"gitDir,omitempty"`
	CommonDir string `json:"commonDir,omitempty"`
	Bare      bool   `json:"bare,omitempty"`
	Branch    string `json:"branch,omitempty"` // refs/heads/<name>, "" when detached
	HeadSHA   string `json:"headSha,omitempty"`
	// Remotes maps remote name to its effective fetch/push URLs.
	Remotes    map[string]RemoteInfo `json:"remotes,omitempty"`
	Sources    map[string]string     `json:"sources,omitempty"`
	Unresolved []Unresolved          `json:"unresolved,omitempty"`
}

// RemoteInfo is the local configuration of one remote, credentials removed.
type RemoteInfo struct {
	URL      string   `json:"url,omitempty"`
	PushURLs []string `json:"pushUrls,omitempty"`
}
