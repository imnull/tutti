package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

// Qwen server contract — see QwenLM/qwen-code docs/developers/qwen-serve-protocol.md.
//
// `qwen serve` exposes an HTTP+SSE daemon on a loopback port (default 4170).
// Every route EXCEPT /health requires an `Authorization: Bearer <token>`
// header matching the value passed to `qwen serve --token` (or
// QWEN_SERVER_TOKEN in the daemon's env). On loopback binds the bearer
// middleware is registered AFTER /health, so the Tutti probe can hit
// /health without a token even when --require-auth is on. This asymmetry is
// the whole reason Health() does not need a bearer while Capabilities() does.

const (
	// QwenServerDefaultURL is the loopback default the qwen-code CLI binds.
	QwenServerDefaultURL = "http://127.0.0.1:4170"

	// QwenServerURLEnv lets operators override the daemon URL when the CLI
	// is launched on a non-default port or behind a tunnel. Tutti does not
	// set this itself; the desktop daemon-spawner (a future commit) is the
	// natural owner.
	QwenServerURLEnv = "QWEN_SERVER_URL"

	// QwenServerTokenEnv is the per-launch bearer the daemon compares against
	// the Authorization header on every non-/health route. Tutti writes a
	// fresh 32-byte hex value here when it spawns qwen serve; the value is
	// scoped to the launch and never logged.
	QwenServerTokenEnv = "QWEN_SERVER_TOKEN"

	// QwenServerRequestTimeout caps every request so a stuck daemon cannot
	// block the dock probe path. Kept low because /capabilities is small
	// JSON and /health is a single status frame.
	QwenServerRequestTimeout = 5 * time.Second
)

// ErrQwenDaemonNotConfigured is returned by the helpers below when the
// environment does not carry enough information to talk to a daemon. The
// caller is expected to surface this to the agentstatus path, which decides
// between "install qwen-code" and "spawn qwen serve".
var ErrQwenDaemonNotConfigured = errors.New("qwen serve daemon is not configured (set QWEN_SERVER_URL and QWEN_SERVER_TOKEN)")

// qwenServerURL resolves the daemon URL from env, falling back to the
// loopback default. The returned value never has a trailing slash so
// callers can concatenate paths directly.
func qwenServerURL() string {
	if v := strings.TrimSpace(os.Getenv(QwenServerURLEnv)); v != "" {
		return strings.TrimRight(v, "/")
	}
	return QwenServerDefaultURL
}

// qwenServerToken returns the bearer token from env, or empty string if
// not set. Callers that require auth MUST check this and refuse cleanly
// rather than sending a malformed Authorization header.
func qwenServerToken() string {
	return strings.TrimSpace(os.Getenv(QwenServerTokenEnv))
}

// QwenDaemonClient is a minimal HTTP+SSE client for a `qwen serve` daemon.
// Constructed per-call (no caching) because the env may rotate between
// probes — the desktop side will likely own a longer-lived instance once
// the daemon-spawner lands, but the agentstatus probe path stays stateless.
type QwenDaemonClient struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// NewQwenDaemonClient returns a client wired to the env-resolved daemon
// URL and bearer token. Callers that need a different endpoint (tests,
// advanced routing) should construct QwenDaemonClient directly so they
// can inject their own HTTPClient.
func NewQwenDaemonClient() QwenDaemonClient {
	return QwenDaemonClient{
		BaseURL:    qwenServerURL(),
		Token:      qwenServerToken(),
		HTTPClient: &http.Client{Timeout: QwenServerRequestTimeout},
	}
}

// Health probes /health. Returns nil on 200 OK; non-nil on transport
// failure or non-200 status. On loopback binds /health is unauthenticated
// per the daemon docs, so we do NOT attach a bearer here — even when
// --require-auth is on, the loopback /health exemption is what lets us
// probe liveness without holding the token.
func (c QwenDaemonClient) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("qwen serve /health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qwen serve /health returned %d", resp.StatusCode)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Capabilities fetches and decodes the /capabilities payload. The shape is
// governed by the daemon's serve capability registry (see
// qwen-serve-protocol.md "Capabilities" section): a top-level `features`
// array of tag names plus a small set of structured fields
// (`workspaceCwd`, `modes.permission`, `caps.workspaceCwd`, etc.). We
// return the raw map so callers can pick fields without coupling to every
// key — the projection logic lives in the capability_catalog /
// model_catalog files.
func (c QwenDaemonClient) Capabilities(ctx context.Context) (map[string]any, error) {
	if c.Token == "" {
		return nil, ErrQwenDaemonNotConfigured
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/capabilities", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qwen serve /capabilities: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("qwen serve /capabilities returned %d: %s", resp.StatusCode, body)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode qwen serve /capabilities: %w", err)
	}
	return payload, nil
}

// IsQwenProvider reports whether a provider string resolves to Qwen Code.
// Used by callers that have a provider string in hand and need to short-
// circuit before reaching for the daemon client. Centralized here so the
// capability/model catalog paths share one truth and stay aligned with
// agentprovider.Normalize.
func IsQwenProvider(provider string) bool {
	return agentprovider.Normalize(provider) == agentprovider.QwenCode
}