package agentstatus

import "runtime"

// qwenPlatform identifies the host platform for the qwen-code CLI.
//
// qwen-code (Alibaba's open-source coding agent, QwenLM/qwen-code) is a pure
// Node 22+ npm distribution; the same `@qwen-code/qwen-code` tarball runs on
// macOS / Linux / Windows and is the entrypoint we resolve via npm-managed
// install (mirroring @openai/codex). This helper is a thin wrapper around
// runtime.GOOS so the rest of the agentstatus code can branch on a small
// closed set instead of repeating platform literals.
type qwenPlatform string

const (
	qwenPlatformDarwin  qwenPlatform = "darwin"
	qwenPlatformLinux   qwenPlatform = "linux"
	qwenPlatformWindows qwenPlatform = "windows"
	qwenPlatformUnknown qwenPlatform = "unknown"
)

// detectQwenPlatform returns the host platform for qwen-code. Unknown GOOS
// values map to qwenPlatformUnknown so callers can refuse install on exotic
// targets rather than silently failing the npm step later.
func detectQwenPlatform() qwenPlatform {
	switch runtime.GOOS {
	case "darwin":
		return qwenPlatformDarwin
	case "linux":
		return qwenPlatformLinux
	case "windows":
		return qwenPlatformWindows
	default:
		return qwenPlatformUnknown
	}
}

// IsSupported reports whether the detected platform is one we are willing to
// install qwen-code on. Anything not in the closed set above returns false.
func (p qwenPlatform) IsSupported() bool {
	return p != qwenPlatformUnknown
}

// String returns the platform identifier used in installer status payloads.
func (p qwenPlatform) String() string {
	return string(p)
}