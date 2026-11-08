# Configuration, logging and redaction notes

Effective configuration follows Git root options/environment, not just cwd.
Load user, project and untracked personal overlays; projects cannot weaken user
policy or set runtime/logging. Trust executable content separately from policy.

JSONL logs are redacted and content-minimized; explicit opt-in raw hook input
is a separate boundary. Text bytes/redaction counts and unavailable/truncated
status must survive normalization and hook preparation. Never store captured
Git output bodies in ordinary invocation logs. Credential-shaped fixtures must
be assembled at runtime, never committed as literals.

Trust-review blobs are a separate private store and are intentionally byte-exact,
unredacted copies. Do not describe them as sanitized logs. See
[security](../security.md) and [hook protocol](../hook-protocol.md).
