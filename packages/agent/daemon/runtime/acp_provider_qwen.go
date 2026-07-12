package agentruntime

// Qwen's ACP provider config (`qwen --acp`). Qwen is Alibaba's Tongyi Qianwen
// (通义千问) CLI that speaks the Agent Client Protocol (JSON-RPC 2.0 over
// stdio). The CLI binary is installed via `npm install -g @qwen-code/qwen-code`.
//
// Unlike most ACP providers (`cursor-agent acp`, `opencode acp`), Qwen uses
// `--acp` as a flag on the main binary, not a subcommand.

import (
	"strings"
)

func NewQwenAdapter(transport ProcessTransport) *standardACPAdapter {
	return NewQwenAdapterWithHostMetadata(transport, LegacyHostMetadata())
}

func NewQwenAdapterWithHostMetadata(transport ProcessTransport, host HostMetadata) *standardACPAdapter {
	return &standardACPAdapter{
		config: standardACPConfig{
			provider:            ProviderQwen,
			adapterName:         "qwen-acp",
			command:             []string{"qwen", "--acp"},
			defaultTitle:        "Qwen",
			defaultTitleAliases: []string{"Qwen", ProviderQwen, "qwen", "qwen-code"},
			authRequiredMessage: "Qwen requires an API key. Set OPENAI_API_KEY or BAILIAN_CODING_PLAN_API_KEY in the environment, or run `qwen` and use `/auth` to sign in.",
			permissionModeID:    qwenACPModeID,
			initializeParams:    func() map[string]any { return defaultACPInitializeParams(host) },
			env:                 func(session Session) []string { return standardACPEnv(session, host) },
		},
		transport: transport,
		host:      host,
		sessions:  make(map[string]*standardACPSession),
	}
}

// qwenACPModeID maps Tutti permission-mode IDs to Qwen's tools.approvalMode
// values. Qwen supports four approval modes:
//
//   - plan:       analyze only, no file modifications or command execution
//   - default:    require approval before file edits or shell commands
//   - auto-edit:  automatically approve file edits; ask for shell commands
//   - yolo:       automatically approve all tool calls
func qwenACPModeID(mode string) string {
	switch strings.TrimSpace(mode) {
	case "plan":
		return "plan"
	case "auto-edit":
		return "auto-edit"
	case "yolo", "full-access":
		return "yolo"
	case "", "default":
		return "default"
	default:
		return ""
	}
}

func qwenACPCommands() []AgentSessionCommand {
	return []AgentSessionCommand{
		{
			Name:        "compact",
			Description: "Compact the conversation context",
		},
		{
			Name:        "review",
			Description: "Review code changes",
			InputHint:   "instructions (optional)",
		},
	}
}
