package agentstatus

import "strings"

// qwenErrorReason classifies a failure string returned by qwen-code (or by
// the `qwen serve` daemon) into the closed set of reason codes Tutti's
// agentstatus surface reports to the desktop UI and the wizard.
//
// The set is intentionally small and mirrors the codex reason codes so the
// renderer side can reuse its existing translation table; new entries only
// land here when qwen-code introduces a category we cannot map.
type qwenErrorReason string

const (
	qwenErrorReasonUnknown              qwenErrorReason = ""
	qwenErrorReasonCLINotFound          qwenErrorReason = "qwen_cli_not_found"
	qwenErrorReasonVersionTooOld        qwenErrorReason = "qwen_version_too_old"
	qwenErrorReasonAuthRequired         qwenErrorReason = "qwen_auth_required"
	qwenErrorReasonAuthInvalid          qwenErrorReason = "qwen_auth_invalid"
	qwenErrorReasonDaemonUnreachable    qwenErrorReason = "qwen_daemon_unreachable"
	qwenErrorReasonDaemonStartupFailure qwenErrorReason = "qwen_daemon_startup_failure"
	qwenErrorReasonInternal             qwenErrorReason = "qwen_internal_error"
)

// classifyQwenError inspects the stderr / error text and returns the
// matching reason code. The check is deliberately keyword-driven: qwen-code
// does not currently expose a stable error-code envelope on the CLI side
// (the `qwen serve` daemon returns JSON-RPC errors but those flow through a
// different path), so we pattern-match against the phrases the CLI emits
// today. Keep the keywords narrow — false positives hurt worse than misses.
func classifyQwenError(stderr string) qwenErrorReason {
	trimmed := strings.TrimSpace(stderr)
	if trimmed == "" {
		return qwenErrorReasonUnknown
	}
	lower := strings.ToLower(trimmed)

	switch {
	case strings.Contains(lower, "command not found"),
		strings.Contains(lower, "not found: qwen"),
		strings.Contains(lower, "no such file"):
		return qwenErrorReasonCLINotFound
	case strings.Contains(lower, "minimum supported version"),
		strings.Contains(lower, "qwen-code version"),
		strings.Contains(lower, "requires qwen"):
		return qwenErrorReasonVersionTooOld
	case strings.Contains(lower, "authentication required"),
		strings.Contains(lower, "not authenticated"),
		strings.Contains(lower, "please run `qwen login`"),
		strings.Contains(lower, "qwen login"):
		return qwenErrorReasonAuthRequired
	case strings.Contains(lower, "invalid api key"),
		strings.Contains(lower, "unauthorized"),
		strings.Contains(lower, "401"):
		return qwenErrorReasonAuthInvalid
	case strings.Contains(lower, "econnrefused 127.0.0.1:4170"),
		strings.Contains(lower, "daemon not running"),
		strings.Contains(lower, "failed to connect to qwen serve"):
		return qwenErrorReasonDaemonUnreachable
	case strings.Contains(lower, "daemon exited"),
		strings.Contains(lower, "qwen serve failed to start"):
		return qwenErrorReasonDaemonStartupFailure
	default:
		return qwenErrorReasonInternal
	}
}