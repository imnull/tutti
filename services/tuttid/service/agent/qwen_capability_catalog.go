package agent

import (
	"context"

	"github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

// discoverQwenCodeCapabilityOptions returns the capability options for the
// Qwen Code provider.
//
// Initial scaffolding: the function returns the fallback skill list so the
// renderer side already shows something meaningful while the real
// integration lands. The eventual implementation will:
//
//   - Spawn `qwen serve --token <bearer>` as a managed subprocess
//     (lifecycle owned by services/tuttid/service/agentstatus/qwen_installer.go).
//   - GET http://127.0.0.1:4170/capabilities with the bearer header.
//   - Parse the `caps.features` block (per
//     QwenLM/qwen-code docs/developers/qwen-serve-protocol.md, the
//     /capabilities payload is the single source of truth for runtime
//     feature discovery — see `caps.features.allow_origin` for an example).
//   - Project each feature into a ComposerCapabilityOption with the
//     same Kind/Status vocabulary the codex path produces.
//
// Until then, this stub is intentionally side-effect-free: it does not
// spawn the daemon, does not block on a port check, and returns the
// fallback list directly so wizard previews stay populated.
func discoverQwenCodeCapabilityOptions(
	ctx context.Context,
	provider string,
	cwd string,
	fallbackSkills []ComposerSkillOption,
) ([]ComposerCapabilityOption, []string) {
	if agentprovider.Normalize(provider) != agentprovider.QwenCode {
		return composerCapabilityCatalogFromSkills(provider, fallbackSkills), nil
	}
	// TODO(qwen): wire up `qwen serve /capabilities` HTTP probe. Track
	// upstream ACP envelope changes via DAEMON_KNOWN_EVENT_TYPE_VALUES
	// in QwenLM/qwen-code packages/sdk-typescript/src/daemon/events.ts.
	return composerCapabilityCatalogFromSkills(provider, fallbackSkills), nil
}