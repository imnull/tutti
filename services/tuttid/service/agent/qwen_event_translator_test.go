package agent

import (
	"encoding/json"
	"errors"
	"testing"
)

// Tests for TranslateQwenEvent (qwen_event_translator.go). The translator
// is the typed bridge between the qwen serve SSE envelope and the
// codegen-fed event protocol. Every supported event type gets a positive
// case; malformed JSON gets an error; unknown types land in Unknown.
//
// The translator returns (event, nil) for unknown but well-formed types —
// matching the upstream SDK's "increment counter, don't abort" policy
// from docs/developers/daemon/09-event-schema.md.

func TestTranslateQwenEvent_session_update(t *testing.T) {
	ev := QwenDaemonEvent{
		Type: "session_update",
		Data: json.RawMessage(`{"sessionId":"s1","sessionUpdate":"agent_message","content":{"text":"hi"}}`),
	}
	got, err := TranslateQwenEvent(ev)
	if err != nil {
		t.Fatalf("TranslateQwenEvent: %v", err)
	}
	if got.SessionUpdate == nil {
		t.Fatal("SessionUpdate nil")
	}
	if got.SessionUpdate.SessionID != "s1" {
		t.Errorf("SessionID: %q", got.SessionUpdate.SessionID)
	}
	if got.SessionUpdate.SessionUpdate != "agent_message" {
		t.Errorf("SessionUpdate: %q", got.SessionUpdate.SessionUpdate)
	}
	if string(got.SessionUpdate.Content) != `{"text":"hi"}` {
		t.Errorf("Content: %s", got.SessionUpdate.Content)
	}
}

func TestTranslateQwenEvent_session_died(t *testing.T) {
	exit := 137
	ev := QwenDaemonEvent{
		Type: "session_died",
		Data: json.RawMessage(`{"sessionId":"s1","reason":"exited","exitCode":137}`),
	}
	got, err := TranslateQwenEvent(ev)
	if err != nil {
		t.Fatalf("TranslateQwenEvent: %v", err)
	}
	if got.SessionDied == nil {
		t.Fatal("SessionDied nil")
	}
	if got.SessionDied.Reason != "exited" {
		t.Errorf("Reason: %q", got.SessionDied.Reason)
	}
	if got.SessionDied.ExitCode == nil || *got.SessionDied.ExitCode != exit {
		t.Errorf("ExitCode: %v", got.SessionDied.ExitCode)
	}
}

func TestTranslateQwenEvent_session_metadata_updated(t *testing.T) {
	ev := QwenDaemonEvent{
		Type: "session_metadata_updated",
		Data: json.RawMessage(`{"sessionId":"s1","displayName":"My Session"}`),
	}
	got, err := TranslateQwenEvent(ev)
	if err != nil {
		t.Fatalf("TranslateQwenEvent: %v", err)
	}
	if got.SessionMetadataUpdated == nil || got.SessionMetadataUpdated.DisplayName != "My Session" {
		t.Errorf("SessionMetadataUpdated: %+v", got.SessionMetadataUpdated)
	}
}

func TestTranslateQwenEvent_permission_request(t *testing.T) {
	ev := QwenDaemonEvent{
		Type: "permission_request",
		Data: json.RawMessage(`{"sessionId":"s1","requestId":"r1","toolCall":{"name":"bash"},"options":[],"originatorClientId":"c1"}`),
	}
	got, err := TranslateQwenEvent(ev)
	if err != nil {
		t.Fatalf("TranslateQwenEvent: %v", err)
	}
	if got.PermissionRequest == nil {
		t.Fatal("PermissionRequest nil")
	}
	if got.PermissionRequest.RequestID != "r1" {
		t.Errorf("RequestID: %q", got.PermissionRequest.RequestID)
	}
	if got.PermissionRequest.OriginatorClientID != "c1" {
		t.Errorf("OriginatorClientID: %q", got.PermissionRequest.OriginatorClientID)
	}
	if string(got.PermissionRequest.ToolCall) != `{"name":"bash"}` {
		t.Errorf("ToolCall: %s", got.PermissionRequest.ToolCall)
	}
}

func TestTranslateQwenEvent_permission_resolved(t *testing.T) {
	ev := QwenDaemonEvent{
		Type: "permission_resolved",
		Data: json.RawMessage(`{"requestId":"r1","outcome":{"decision":"allow"}}`),
	}
	got, err := TranslateQwenEvent(ev)
	if err != nil {
		t.Fatalf("TranslateQwenEvent: %v", err)
	}
	if got.PermissionResolved == nil || got.PermissionResolved.RequestID != "r1" {
		t.Errorf("PermissionResolved: %+v", got.PermissionResolved)
	}
}

func TestTranslateQwenEvent_model_switched(t *testing.T) {
	ev := QwenDaemonEvent{
		Type: "model_switched",
		Data: json.RawMessage(`{"sessionId":"s1","modelId":"qwen3-coder-plus"}`),
	}
	got, err := TranslateQwenEvent(ev)
	if err != nil {
		t.Fatalf("TranslateQwenEvent: %v", err)
	}
	if got.ModelSwitched == nil || got.ModelSwitched.ModelID != "qwen3-coder-plus" {
		t.Errorf("ModelSwitched: %+v", got.ModelSwitched)
	}
}

func TestTranslateQwenEvent_model_switch_failed(t *testing.T) {
	ev := QwenDaemonEvent{
		Type: "model_switch_failed",
		Data: json.RawMessage(`{"sessionId":"s1","requestedModelId":"x","error":"not allowed"}`),
	}
	got, err := TranslateQwenEvent(ev)
	if err != nil {
		t.Fatalf("TranslateQwenEvent: %v", err)
	}
	if got.ModelSwitchFailed == nil || got.ModelSwitchFailed.Error != "not allowed" {
		t.Errorf("ModelSwitchFailed: %+v", got.ModelSwitchFailed)
	}
}

func TestTranslateQwenEvent_session_snapshot(t *testing.T) {
	ev := QwenDaemonEvent{
		Type: "session_snapshot",
		Data: json.RawMessage(`{"sessionId":"s1","currentModelId":"qwen3-coder","currentApprovalMode":"default"}`),
	}
	got, err := TranslateQwenEvent(ev)
	if err != nil {
		t.Fatalf("TranslateQwenEvent: %v", err)
	}
	if got.Snapshot == nil || got.Snapshot.CurrentModelID != "qwen3-coder" {
		t.Errorf("Snapshot: %+v", got.Snapshot)
	}
}

func TestTranslateQwenEvent_unknownTypeLandsInUnknown(t *testing.T) {
	// qwen-code's closed type set is wider than what we model today;
	// the SDK policy is "increment counter, don't abort". Mirror that.
	ev := QwenDaemonEvent{
		Type: "some_future_event_type",
		Data: json.RawMessage(`{"future":true}`),
	}
	got, err := TranslateQwenEvent(ev)
	if err != nil {
		t.Fatalf("TranslateQwenEvent: %v", err)
	}
	if got.Unknown == nil {
		t.Fatal("Unknown nil for unmodeled type")
	}
	if got.Unknown.Type != "some_future_event_type" {
		t.Errorf("Unknown.Type: %q", got.Unknown.Type)
	}
	if string(got.Unknown.Raw) != `{"future":true}` {
		t.Errorf("Unknown.Raw: %s", got.Unknown.Raw)
	}
}

func TestTranslateQwenEvent_malformedJSONReturnsError(t *testing.T) {
	ev := QwenDaemonEvent{
		Type: "session_update",
		Data: json.RawMessage(`{"sessionId":`), // truncated
	}
	_, err := TranslateQwenEvent(ev)
	if err == nil {
		t.Fatal("malformed JSON: want error, got nil")
	}
	if errors.Is(err, ErrQwenEventTypeUnknown) {
		t.Errorf("malformed JSON should NOT be classified as ErrQwenEventTypeUnknown, got %v", err)
	}
}

func TestTranslateQwenEvent_emptyTypeIsUnknown(t *testing.T) {
	// Defensive: if the SSE parser somehow yields a record without a
	// type field (corrupt daemon, mid-rollout), we should treat it as
	// unknown rather than panic.
	ev := QwenDaemonEvent{
		Type: "",
		Data: json.RawMessage(`{"x":1}`),
	}
	got, err := TranslateQwenEvent(ev)
	if err != nil {
		t.Fatalf("TranslateQwenEvent: %v", err)
	}
	if got.Unknown == nil || got.Unknown.Type != "" {
		t.Errorf("empty type should land in Unknown, got %+v", got)
	}
}
