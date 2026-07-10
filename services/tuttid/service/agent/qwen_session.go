package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Session lifecycle for `qwen serve` — POST /session (create) and
// DELETE /session/:id (close). Per QwenLM/qwen-code docs/developers/qwen-serve-protocol.md,
// every non-/health route requires `Authorization: Bearer <token>` matching
// the value passed to `qwen serve --token`. The session id returned by
// Create is the selector for every subsequent per-session route, including
// the SSE event stream consumed by SubscribeSessionEvents
// (qwen_session_events.go).

// QwenSessionCreateOptions captures the body for POST /session. Field
// semantics per the daemon docs:
//
//   - CWD: absolute path to a registered workspace. When empty, the daemon
//     uses workspaceCwd from /capabilities (single-workspace daemons).
//   - SessionScope: "primary" | "additional" | "shared". Older daemons
//     silently ignore it, so SDK clients should pre-flight
//     caps.features.session_scope_override before sending.
//   - ClientInfo: name+version surfaced to the daemon for telemetry /
//     capability negotiation. Tutti passes {"name":"tuttid", "version":...}.
type QwenSessionCreateOptions struct {
	CWD          string
	SessionScope string
	ClientInfo   map[string]string
}

// QwenSessionHandle is the post-create reference. ID is the selector for
// every other per-session route. WorkspaceID / WorkspaceCwd are echoed
// back from the daemon so callers can confirm the session landed on the
// expected workspace (the daemon returns 409 SessionWorkspaceConflictError
// if not — see qwen-serve-protocol.md "SessionWorkspaceConflictError").
type QwenSessionHandle struct {
	ID           string
	WorkspaceID  string
	WorkspaceCwd string
}

// CreateSession opens a new ACP session with the qwen serve daemon.
// Returns the session id on success; the daemon-generated 5xx with
// {error, code, data} envelope is surfaced as a wrapped error with the
// code/message visible.
func (c QwenDaemonClient) CreateSession(ctx context.Context, opts QwenSessionCreateOptions) (QwenSessionHandle, error) {
	if c.Token == "" {
		return QwenSessionHandle{}, ErrQwenDaemonNotConfigured
	}
	body := map[string]any{}
	if opts.CWD != "" {
		body["cwd"] = opts.CWD
	}
	if opts.SessionScope != "" {
		body["sessionScope"] = opts.SessionScope
	}
	if len(opts.ClientInfo) > 0 {
		body["clientInfo"] = opts.ClientInfo
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return QwenSessionHandle{}, fmt.Errorf("marshal qwen session create body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/session", bytes.NewReader(payload))
	if err != nil {
		return QwenSessionHandle{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return QwenSessionHandle{}, fmt.Errorf("qwen serve POST /session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return QwenSessionHandle{}, fmt.Errorf("qwen serve /session returned %d: %s", resp.StatusCode, body)
	}
	var raw struct {
		SessionID    string `json:"sessionId"`
		WorkspaceID  string `json:"workspaceId"`
		WorkspaceCwd string `json:"workspaceCwd"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return QwenSessionHandle{}, fmt.Errorf("decode qwen serve /session response: %w", err)
	}
	if strings_TrimSpaceIsEmpty(raw.SessionID) {
		return QwenSessionHandle{}, errors.New("qwen serve /session response missing sessionId")
	}
	return QwenSessionHandle{
		ID:           raw.SessionID,
		WorkspaceID:  raw.WorkspaceID,
		WorkspaceCwd: raw.WorkspaceCwd,
	}, nil
}

// CloseSession terminates the session via DELETE /session/:id. Idempotent:
// a 404 from a session we never created (or already closed) is treated as
// success because the caller's post-condition — "no live session on this
// id" — is already satisfied.
func (c QwenDaemonClient) CloseSession(ctx context.Context, sessionID string) error {
	if c.Token == "" {
		return ErrQwenDaemonNotConfigured
	}
	if strings_TrimSpaceIsEmpty(sessionID) {
		return errors.New("qwen session id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+"/session/"+sessionID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("qwen serve DELETE /session/%s: %w", sessionID, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("qwen serve DELETE /session/%s returned %d: %s", sessionID, resp.StatusCode, body)
	}
}

// strings_TrimSpaceIsEmpty is a tiny local helper so this file does not
// pull in strings just for one TrimSpace check. (Centralized so the
// qwen_session*.go trio reads coherently without importing strings twice.)
func strings_TrimSpaceIsEmpty(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}
