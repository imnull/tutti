package agentstatus

import (
	"strconv"
	"strings"
)

// MinSupportedQwenCodeVersion is the lowest qwen-code version Tutti supports.
//
// Capability-derived: qwen-code 0.19.0 is the release that promoted the
// `qwen serve` daemon (HTTP+SSE on 127.0.0.1:4170) and the typed event schema
// v1 to the public integration surface; before that floor the daemon protocol
// is not stable, and the Tutti-side probe would race an unfinished handshake.
// Below this version a detected qwen-code is flagged as too old (surfaced as
// QWEN_VERSION_TOO_OLD) and the server-side 400 is the backstop. Bump this
// constant when raising the floor; nothing else needs to change.
const MinSupportedQwenCodeVersion = "0.19.0"

// compareQwenCodeVersions compares two semver-ish version strings.
//
// It returns -1, 0, or 1 (a<b, a==b, a>b) and ok=true when both parse. A
// leading "v" is tolerated and missing components default to 0. A pre-release
// suffix (after "-") sorts below the same release core. ok is false when
// either version cannot be parsed into a numeric core.
func compareQwenCodeVersions(a, b string) (int, bool) {
	coreA, preA, okA := parseQwenCodeVersion(a)
	coreB, preB, okB := parseQwenCodeVersion(b)
	if !okA || !okB {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		if coreA[i] != coreB[i] {
			if coreA[i] < coreB[i] {
				return -1, true
			}
			return 1, true
		}
	}
	switch {
	case preA && !preB:
		return -1, true
	case !preA && preB:
		return 1, true
	default:
		return 0, true
	}
}

// qwenCodeVersionMeetsMinimum reports whether version satisfies
// MinSupportedQwenCodeVersion. An empty or unparseable version is treated as
// "unknown" and is NOT flagged as too old here — binary/CLI presence checks
// cover the missing case, and the server-side error is the backstop.
func qwenCodeVersionMeetsMinimum(version string) bool {
	cmp, ok := compareQwenCodeVersions(version, MinSupportedQwenCodeVersion)
	if !ok {
		return true
	}
	return cmp >= 0
}

// parseQwenCodeVersion returns the [major, minor, patch] core, whether a
// pre-release suffix is present, and ok when the core parsed.
func parseQwenCodeVersion(version string) ([3]int, bool, bool) {
	v := strings.TrimSpace(version)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return [3]int{}, false, false
	}
	pre := false
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		pre = v[idx] == '-'
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return [3]int{}, false, false
	}
	var core [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return [3]int{}, false, false
		}
		core[i] = n
	}
	return core, pre, true
}