package agent

import (
	"context"
	"log/slog"
)

// discoverQwenCodeCapabilityOptions returns the capability options for the
// Qwen Code provider.
//
// Flow (per QwenLM/qwen-code docs/developers/qwen-serve-protocol.md):
//
//	1. Probe GET /health — confirms the daemon is up. Loopback /health is
//	   unauthenticated, so this works even when --require-auth is on.
//	2. GET /capabilities with the bearer — returns the feature tag array
//	   plus a small set of structured fields (`workspaceCwd`,
//	   `modes.permission`, etc.).
//	3. Project the feature tags into ComposerCapabilityOption rows.
//
// Each advertised feature tag becomes one ComposerCapabilityOption with
// Kind="qwen-feature" so the dock can light up the daemon's surface.
// Unknown / older shapes fall back to the skill list so the UI never goes
// empty during a daemon upgrade window.
func discoverQwenCodeCapabilityOptions(
	ctx context.Context,
	provider string,
	cwd string,
	fallbackSkills []ComposerSkillOption,
) ([]ComposerCapabilityOption, []string) {
	if !IsQwenProvider(provider) {
		return composerCapabilityCatalogFromSkills(provider, fallbackSkills), nil
	}

	fallback := composerCapabilityCatalogFromSkills(provider, fallbackSkills)
	client := NewQwenDaemonClient()

	if err := client.Health(ctx); err != nil {
		slog.Warn(
			"qwen daemon health probe failed; using fallback capability catalog",
			"provider", provider,
			"err", err,
		)
		return fallback, []string{err.Error()}
	}

	caps, err := client.Capabilities(ctx)
	if err != nil {
		slog.Warn(
			"qwen daemon capabilities fetch failed; using fallback capability catalog",
			"provider", provider,
			"err", err,
		)
		return fallback, []string{err.Error()}
	}

	return projectQwenCapabilitiesToComposerOptions(caps, fallback), nil
}

// projectQwenCapabilitiesToComposerOptions turns the raw /capabilities
// payload into the shared ComposerCapabilityOption vocabulary.
//
// Today this emits one row per advertised feature tag. Future iterations
// will fold the modes.permission block and the workspace_mcp* /
// workspace_skills features into richer typed rows (skill / mcpServer
// kinds) so the renderer's existing codex-handling can be reused without
// provider branching.
func projectQwenCapabilitiesToComposerOptions(
	caps map[string]any,
	fallback []ComposerCapabilityOption,
) []ComposerCapabilityOption {
	out := append([]ComposerCapabilityOption(nil), fallback...)

	rawFeatures, _ := caps["features"].([]any)
	for _, raw := range rawFeatures {
		name, ok := raw.(string)
		if !ok || name == "" {
			continue
		}
		out = append(out, ComposerCapabilityOption{
			ID:         "qwen-feature:" + name,
			Kind:       "qwen-feature",
			Name:       name,
			Label:      name,
			Status:     "available",
			Invocation: "none",
		})
	}
	return out
}