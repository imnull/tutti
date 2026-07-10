package agent

import (
	"context"

	"github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

// discoverQwenCodeModelCatalog returns the model catalog for the Qwen Code
// provider.
//
// Initial scaffolding: returns an empty ModelCatalog. The eventual
// implementation will hit `qwen serve /capabilities` and project the
// provider's reported model list into the shared ModelCatalog shape used by
// codex/opencode.
//
// qwen-code is multi-protocol — OpenAI / Anthropic / Gemini / Qwen native
// + Ollama / vLLM local — so the catalog is provider-driven, not hard-coded.
// The `currentModelId` field on the SSE event envelope
// (DAEMON_KNOWN_EVENT_TYPE_VALUES `session_snapshot`) is what tells the UI
// which model the active session is bound to.
func discoverQwenCodeModelCatalog(
	_ context.Context,
	provider string,
	_ string,
) (ModelCatalog, []string) {
	if agentprovider.Normalize(provider) != agentprovider.QwenCode {
		return ModelCatalog{}, nil
	}
	// TODO(qwen): wire up `qwen serve /capabilities` HTTP probe and project
	// the `models[]` block into ModelCatalog. The codex path is the
	// reference shape — keep the field names aligned so the renderer can
	// share a row component across providers.
	return ModelCatalog{}, nil
}