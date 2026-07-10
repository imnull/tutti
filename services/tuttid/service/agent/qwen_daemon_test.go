package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Tests for QwenDaemonClient (qwen_daemon.go). The client talks to a
// `qwen serve` daemon over HTTP+SSE; we use httptest to stand in for
// the daemon. Each test isolates one behavior:
//
//   - env resolution (QWEN_SERVER_URL / QWEN_SERVER_TOKEN)
//   - /health probe
//   - /capabilities fetch + JSON decode
//   - error envelopes
//
// No real network. No flakes.

func TestQwenServerURL_defaultWhenEnvUnset(t *testing.T) {
	t.Setenv(QwenServerURLEnv, "")
	if got := qwenServerURL(); got != QwenServerDefaultURL {
		t.Errorf("default URL: want %q, got %q", QwenServerDefaultURL, got)
	}
}

func TestQwenServerURL_trimsTrailingSlash(t *testing.T) {
	t.Setenv(QwenServerURLEnv, "http://localhost:5000/")
	if got := qwenServerURL(); got != "http://localhost:5000" {
		t.Errorf("trailing slash: want %q, got %q", "http://localhost:5000", got)
	}
}

func TestQwenServerURL_trimsWhitespace(t *testing.T) {
	t.Setenv(QwenServerURLEnv, "  http://localhost:5000  ")
	if got := qwenServerURL(); got != "http://localhost:5000" {
		t.Errorf("whitespace: want %q, got %q", "http://localhost:5000", got)
	}
}

func TestQwenServerToken_trimsWhitespace(t *testing.T) {
	t.Setenv(QwenServerTokenEnv, "  secret\n")
	if got := qwenServerToken(); got != "secret" {
		t.Errorf("token: want %q, got %q", "secret", got)
	}
}

func TestNewQwenDaemonClient_wiresEnv(t *testing.T) {
	t.Setenv(QwenServerURLEnv, "http://daemon.local:9000")
	t.Setenv(QwenServerTokenEnv, "bearer")
	c := NewQwenDaemonClient()
	if c.BaseURL != "http://daemon.local:9000" {
		t.Errorf("BaseURL: got %q", c.BaseURL)
	}
	if c.Token != "bearer" {
		t.Errorf("Token: got %q", c.Token)
	}
	if c.HTTPClient == nil {
		t.Errorf("HTTPClient: nil")
	}
}

func TestQwenDaemonClient_Health_returnsNilOn200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := QwenDaemonClient{BaseURL: srv.URL, Token: "t", HTTPClient: srv.Client()}
	if err := c.Health(context.Background()); err != nil {
		t.Errorf("Health 200: got %v", err)
	}
}

func TestQwenDaemonClient_Health_returnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := QwenDaemonClient{BaseURL: srv.URL, Token: "t", HTTPClient: srv.Client()}
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("Health 500: want error, got nil")
	}
}

func TestQwenDaemonClient_Health_returnsErrorOnConnectionRefused(t *testing.T) {
	// Bind a server then close it so the port is guaranteed-free; this
	// makes the test deterministic across platforms (no TIME_WAIT flake).
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()

	c := QwenDaemonClient{BaseURL: srv.URL, Token: "t", HTTPClient: &http.Client{}}
	if err := c.Health(context.Background()); err == nil {
		t.Fatal("Health refused: want error, got nil")
	}
}

func TestQwenDaemonClient_Capabilities_requiresToken(t *testing.T) {
	c := QwenDaemonClient{BaseURL: "http://127.0.0.1:1", Token: "", HTTPClient: http.DefaultClient}
	if _, err := c.Capabilities(context.Background()); err != ErrQwenDaemonNotConfigured {
		t.Errorf("missing token: want ErrQwenDaemonNotConfigured, got %v", err)
	}
}

func TestQwenDaemonClient_Capabilities_attachesBearerHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"features":["health","session_create"]}`))
	}))
	defer srv.Close()

	c := QwenDaemonClient{BaseURL: srv.URL, Token: "my-secret", HTTPClient: srv.Client()}
	caps, err := c.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if gotAuth != "Bearer my-secret" {
		t.Errorf("Authorization header: want %q, got %q", "Bearer my-secret", gotAuth)
	}
	features, _ := caps["features"].([]any)
	if len(features) != 2 {
		t.Errorf("features: want 2 entries, got %d", len(features))
	}
}

func TestQwenDaemonClient_Capabilities_returnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"token_required"}`))
	}))
	defer srv.Close()

	c := QwenDaemonClient{BaseURL: srv.URL, Token: "x", HTTPClient: srv.Client()}
	if _, err := c.Capabilities(context.Background()); err == nil {
		t.Fatal("Capabilities 401: want error, got nil")
	}
}

func TestQwenDaemonClient_Capabilities_returnsErrorOnMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	c := QwenDaemonClient{BaseURL: srv.URL, Token: "x", HTTPClient: srv.Client()}
	if _, err := c.Capabilities(context.Background()); err == nil {
		t.Fatal("Capabilities malformed: want error, got nil")
	}
}

func TestQwenDaemonClient_Capabilities_decodesTypedFields(t *testing.T) {
	// Validates that the /capabilities decoder keeps structured fields
	// (workspaceCwd, modes.permission) intact so downstream projection
	// has something to work with.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"features":     []string{"health", "capabilities"},
			"workspaceCwd": "/abs/path",
			"modes": map[string]any{
				"permission": map[string]any{
					"modes": []string{"first-responder", "consensus"},
				},
			},
		})
	}))
	defer srv.Close()

	c := QwenDaemonClient{BaseURL: srv.URL, Token: "x", HTTPClient: srv.Client()}
	caps, err := c.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if got, _ := caps["workspaceCwd"].(string); got != "/abs/path" {
		t.Errorf("workspaceCwd: want /abs/path, got %q", got)
	}
}

func TestIsQwenProvider_normalizesAliases(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"qwen", true},
		{"qwen-code", true},
		{"qwen-cli", true},
		{"QWEN", true},
		{"  qwen-code  ", true},
		{"codex", false},
		{"claude", false},
		{"", false},
		{"unknown", false},
	}
	for _, c := range cases {
		if got := IsQwenProvider(c.in); got != c.want {
			t.Errorf("IsQwenProvider(%q): want %v, got %v", c.in, c.want, got)
		}
	}
}
