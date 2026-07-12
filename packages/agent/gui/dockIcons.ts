import {
  codexRoundedUrl,
  cursorRoundedUrl,
  hermesRoundedUrl,
  manageAgentClaudeCodeUrl,
  manageAgentTuttiUrl,
  opencodeRoundedUrl,
  openclawRoundedUrl,
  tuttiAgentRoundedUrl
} from "./managedAgentIconAssets.ts";

export const agentGuiDockIconUrl = codexRoundedUrl;

export const agentGuiDockIconUrls = {
  "claude-code": manageAgentClaudeCodeUrl,
  codex: codexRoundedUrl,
  cursor: cursorRoundedUrl,
  hermes: hermesRoundedUrl,
  nexight: manageAgentTuttiUrl,
  openclaw: openclawRoundedUrl,
  opencode: opencodeRoundedUrl,
  qwen: manageAgentTuttiUrl,
  "tutti-agent": tuttiAgentRoundedUrl
} as const;
