package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Tests for parseQwenSSEStream. We exercise the parser directly because
// it is the most failure-prone piece of the qwen integration (the daemon
// wire format is rigid; a single missed blank line or stray comment and
// every subsequent event parses wrong). No daemon required.
//
// Each test feeds a synthetic SSE body and asserts on the resulting
// QwenDaemonEvent slice. The test cases mirror the canonical examples
// from QwenLM/qwen-code docs/developers/daemon/09-event-schema.md.

func TestParseQwenSSEStream_singleRecord(t *testing.T) {
	body := strings.NewReader(
		"id: 1\n" +
			"event: session_update\n" +
			`data: {"sessionId":"s1","content":"hello"}` + "\n" +
			"\n",
	)
	var got []QwenDaemonEvent
	if err := parseQwenSSEStream(context.Background(), body, func(_ context.Context, ev QwenDaemonEvent) error {
		got = append(got, ev)
		return nil
	}); err != nil {
		t.Fatalf("parseQwenSSEStream: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	want := QwenDaemonEvent{
		ID:            1,
		Type:          "session_update",
		Data:          json.RawMessage(`{"sessionId":"s1","content":"hello"}`),
		SchemaVersion: 1,
	}
	if got[0].ID != want.ID {
		t.Errorf("ID: want %d, got %d", want.ID, got[0].ID)
	}
	if got[0].Type != want.Type {
		t.Errorf("Type: want %q, got %q", want.Type, got[0].Type)
	}
	if string(got[0].Data) != string(want.Data) {
		t.Errorf("Data: want %s, got %s", want.Data, got[0].Data)
	}
	if got[0].SchemaVersion != 1 {
		t.Errorf("SchemaVersion: want 1, got %d", got[0].SchemaVersion)
	}
}

func TestParseQwenSSEStream_multilineDataJoinedWithNewline(t *testing.T) {
	// Per SSE spec §Interpreting the Event Stream: multiple "data:" lines
	// are concatenated with "\n" before being passed to the event handler.
	// qwen-code emits multi-line data for tool-call stacks and large
	// reasoning traces; the parser must preserve the newlines verbatim.
	body := strings.NewReader(
		"id: 7\n" +
			"event: session_update\n" +
			"data: line-one\n" +
			"data: line-two\n" +
			"data: line-three\n" +
			"\n",
	)
	var got []QwenDaemonEvent
	if err := parseQwenSSEStream(context.Background(), body, func(_ context.Context, ev QwenDaemonEvent) error {
		got = append(got, ev)
		return nil
	}); err != nil {
		t.Fatalf("parseQwenSSEStream: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	want := "line-one\nline-two\nline-three"
	if string(got[0].Data) != want {
		t.Errorf("Data: want %q, got %q", want, got[0].Data)
	}
}

func TestParseQwenSSEStream_syntheticFrameHasZeroID(t *testing.T) {
	// qwen-code's slow_client_warning / replay_complete / state_resync_required
	// frames are force-pushed WITHOUT an id (per the daemon docs). The
	// parser must leave ID at 0 so downstream handlers can branch on it
	// the same way they branch on the upstream SDK's `id === undefined`.
	body := strings.NewReader(
		"event: slow_client_warning\n" +
			`data: {"queueSize":900,"maxQueued":1024}` + "\n" +
			"\n",
	)
	var got []QwenDaemonEvent
	if err := parseQwenSSEStream(context.Background(), body, func(_ context.Context, ev QwenDaemonEvent) error {
		got = append(got, ev)
		return nil
	}); err != nil {
		t.Fatalf("parseQwenSSEStream: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	if got[0].ID != 0 {
		t.Errorf("synthetic frame ID: want 0, got %d", got[0].ID)
	}
	if got[0].Type != "slow_client_warning" {
		t.Errorf("Type: want slow_client_warning, got %q", got[0].Type)
	}
}

func TestParseQwenSSEStream_ignoresCommentLines(t *testing.T) {
	// Per SSE spec, lines starting with ":" are heartbeats / comments and
	// must not produce an event. The parser sees them between records and
	// must skip cleanly without losing the next record's id/event fields.
	body := strings.NewReader(
		": this is a heartbeat\n" +
			"id: 42\n" +
			"event: session_update\n" +
			`data: {"ok":true}` + "\n" +
			"\n",
	)
	var got []QwenDaemonEvent
	if err := parseQwenSSEStream(context.Background(), body, func(_ context.Context, ev QwenDaemonEvent) error {
		got = append(got, ev)
		return nil
	}); err != nil {
		t.Fatalf("parseQwenSSEStream: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 event (comments ignored), got %d", len(got))
	}
	if got[0].ID != 42 {
		t.Errorf("ID: want 42, got %d", got[0].ID)
	}
}

func TestParseQwenSSEStream_multipleRecordsInOrder(t *testing.T) {
	// Verifies the parser correctly resets between records: a new id
	// overwrites the previous one, and dataBuf is cleared at every
	// blank-line boundary. Out-of-order or merged records would surface
	// here as duplicate ids or merged data bodies.
	body := strings.NewReader(
		"id: 1\n" +
			"event: session_update\n" +
			`data: {"n":1}` + "\n" +
			"\n" +
			"id: 2\n" +
			"event: session_update\n" +
			`data: {"n":2}` + "\n" +
			"\n" +
			"id: 3\n" +
			"event: session_died\n" +
			`data: {"reason":"client_close"}` + "\n" +
			"\n",
	)
	var got []QwenDaemonEvent
	if err := parseQwenSSEStream(context.Background(), body, func(_ context.Context, ev QwenDaemonEvent) error {
		got = append(got, ev)
		return nil
	}); err != nil {
		t.Fatalf("parseQwenSSEStream: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 events, got %d", len(got))
	}
	if got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 {
		t.Errorf("IDs out of order: %d/%d/%d", got[0].ID, got[1].ID, got[2].ID)
	}
	if got[2].Type != "session_died" {
		t.Errorf("last event Type: want session_died, got %q", got[2].Type)
	}
}

func TestParseQwenSSEStream_handlerErrorPropagates(t *testing.T) {
	// Returning an error from the handler must abort the stream and
	// surface the error to the caller — used by the bridge layer to
	// stop a subscription when Tutti's renderer disconnects.
	sentinel := errors.New("downstream closed")
	body := strings.NewReader(
		"id: 1\n" +
			"event: session_update\n" +
			`data: {"x":1}` + "\n" +
			"\n" +
			"id: 2\n" +
			"event: session_update\n" +
			`data: {"x":2}` + "\n" +
			"\n",
	)
	var count int
	err := parseQwenSSEStream(context.Background(), body, func(_ context.Context, _ QwenDaemonEvent) error {
		count++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error, got %v", err)
	}
	if count != 1 {
		t.Errorf("handler should have been called exactly once, got %d", count)
	}
}

func TestParseQwenSSEStream_stripsLeadingSpaceOnData(t *testing.T) {
	// Per SSE spec: if a field value starts with a single space (after
	// the colon separator), strip exactly one space. The colon-then-space
	// is the protocol's "this is data, not a field name" sentinel.
	body := strings.NewReader(
		"id: 1\n" +
			"event: session_update\n" +
			"data: {\"leading\":true}\n" +
			"\n",
	)
	var got []QwenDaemonEvent
	if err := parseQwenSSEStream(context.Background(), body, func(_ context.Context, ev QwenDaemonEvent) error {
		got = append(got, ev)
		return nil
	}); err != nil {
		t.Fatalf("parseQwenSSEStream: %v", err)
	}
	if string(got[0].Data) != `{"leading":true}` {
		t.Errorf("Data: want %q, got %q", `{"leading":true}`, got[0].Data)
	}
}
