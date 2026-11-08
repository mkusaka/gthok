# Process and stdin notes

Inherit descriptors unless supervision/capture/replay is required. Preserve Git
status and re-raise its terminating signal; post hooks do not replace it. Capture
is bounded, binary-safe and opt-in, and pipes affect TTY behavior.

Do not read bidirectional update-ref, cat-file batch, fast-import or credential
pipes ahead to EOF. Finite regular files can be analyzed where supported;
replay captured bytes exactly and keep temporary storage private. Timeout/size
limits must produce explicit unavailable input, not a fabricated empty stream.
Delegate fallback requires retrySafe plus no unsafe partial output; see
[delegates](../delegates.md) and [exit status](../exit-status.md).
