package agent

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Typed bridge between qwen serve's SSE event stream and Tutti's internal
// event types.
//
// Pipeline:
//
//	qwen serve SSE → SubscribeSessionEvents → QwenDaemonEvent
//	    → TranslateQwenEvent (this file) → typed event struct
//	    → [TODO] publish on Tutti's renderer-facing event bus
//
// Why this layer exists:
//   - QwenDaemonEvent.Data is json.RawMessage: the wire shape depends on
//     the envelope `type` field, and Tutti's renderer / dock / composer
//     each want a typed Go struct, not a raw map.
//   - The translation table is the single source of truth for which qwen
//     event types Tutti understands. Unknown types surface as
//     ErrQwenEventTypeUnknown so the bridge layer can emit a
//     `unrecognizedKnownEventCount` synthetic frame (matching the SDK's
//     forward-compat behavior described in 09-event-schema.md).
//   - The typed structs are deliberately NOT named after Tutti's existing
//     event types because the codegen'd bridge from codex app-server
//     owns the canonical mapping. These structs are the input the bridge
//     will receive once it lands; renaming them is a sed-level change.

// QwenSessionUpdateEvent is the typed payload of a `session_update` frame.
// Per the daemon schema:
//
//	{
//	  "sessionUpdate": "<string>",  // ACP session-update kind
//	  "content": <any>              // shape depends on sessionUpdate
//	}
//
// We only carry the discriminator + raw content; downstream consumers
// (the bridge, the renderer) decode `content` against the specific
// sessionUpdate kind they care about. This keeps the translator free of
// ACP-specific knowledge.
type QwenSessionUpdateEvent struct {
	SessionID     string
	SessionUpdate string
	Content       json.RawMessage
}

// QwenSessionDiedEvent is the typed payload of a `session_died` frame.
// Terminal: the session is gone on the daemon side; the bridge layer
// should tear down the subscription and surface the reason to the
// renderer.
type QwenSessionDiedEvent struct {
	SessionID  string
	Reason     string
	ExitCode   *int
	SignalCode *int
}

// QwenSessionMetadataUpdatedEvent is the typed payload of a
// `session_metadata_updated` frame. Non-terminal; the bridge updates the
// cached session record.
type QwenSessionMetadataUpdatedEvent struct {
	SessionID   string
	DisplayName string
}

// QwenPermissionRequestEvent is the typed payload of `permission_request`.
// The renderer turns this into a UI prompt; the bridge forwards the
// user's vote via `POST /session/:id/permissions/vote` (route documented
// separately in the daemon protocol).
type QwenPermissionRequestEvent struct {
	SessionID          string
	RequestID          string
	ToolCall           json.RawMessage
	Options            []json.RawMessage
	OriginatorClientID string
}

// QwenPermissionResolvedEvent is the typed payload of
// `permission_resolved`. The renderer closes the corresponding UI
// prompt.
type QwenPermissionResolvedEvent struct {
	RequestID string
	Outcome   json.RawMessage
}

// QwenModelSwitchedEvent is the typed payload of `model_switched`. The
// bridge updates the composer / dock model badge.
type QwenModelSwitchedEvent struct {
	SessionID string
	ModelID   string
}

// QwenModelSwitchFailedEvent is the typed payload of `model_switch_failed`.
// The bridge surfaces a transient toast and reverts the composer.
type QwenModelSwitchFailedEvent struct {
	SessionID        string
	RequestedModelID string
	Error            string
}

// QwenSnapshotEvent is the typed payload of `session_snapshot`. Synthetic
// (no id); the bridge uses it as a "current state" checkpoint emitted
// after SSE attach / replay.
type QwenSnapshotEvent struct {
	SessionID           string
	CurrentModelID      string
	CurrentApprovalMode string
}

// QwenUnknownEvent surfaces any event type that this translator does
// not yet model. The bridge should record it as
// `unrecognizedKnownEventCount` (matching the SDK's behavior for unknown
// but known-schema types) rather than dropping it on the floor — losing
// events makes forward-compat harder to debug.
type QwenUnknownEvent struct {
	Type string
	Raw  json.RawMessage
}

// QwenTranslatedEvent is the union envelope emitted by TranslateQwenEvent.
// Exactly one of the pointer fields is non-nil. The bridge layer type-
// switches on this.
type QwenTranslatedEvent struct {
	SessionUpdate          *QwenSessionUpdateEvent
	SessionDied            *QwenSessionDiedEvent
	SessionMetadataUpdated *QwenSessionMetadataUpdatedEvent
	PermissionRequest      *QwenPermissionRequestEvent
	PermissionResolved     *QwenPermissionResolvedEvent
	ModelSwitched          *QwenModelSwitchedEvent
	ModelSwitchFailed      *QwenModelSwitchFailedEvent
	Snapshot               *QwenSnapshotEvent
	Unknown                *QwenUnknownEvent
}

// ErrQwenEventTypeUnknown is returned when the event's `type` field is
// not in the closed translation table. The bridge should treat this as
// "forward-compatible unknown" and continue the stream rather than
// tearing down.
var ErrQwenEventTypeUnknown = errors.New("qwen event type not in translation table")

// TranslateQwenEvent decodes the envelope's Data field against the type
// table and returns the typed event. The first error return is reserved
// for envelope-level failures (malformed JSON, missing required fields).
// Unknown-but-valid event types return (QwenTranslatedEvent{Unknown: ...},
// nil) — never ErrQwenEventTypeUnknown — so the bridge's switch can
// decide whether to record a count, log, or both.
//
// The reason: the daemon's known-types set is wider than the subset we
// model here, and the SDK's stated policy is "increment counter, don't
// abort". Mirror that.
func TranslateQwenEvent(ev QwenDaemonEvent) (QwenTranslatedEvent, error) {
	switch ev.Type {
	case "session_update":
		var raw struct {
			SessionID     string          `json:"sessionId"`
			SessionUpdate string          `json:"sessionUpdate"`
			Content       json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(ev.Data, &raw); err != nil {
			return QwenTranslatedEvent{}, fmt.Errorf("decode session_update: %w", err)
		}
		return QwenTranslatedEvent{
			SessionUpdate: &QwenSessionUpdateEvent{
				SessionID:     raw.SessionID,
				SessionUpdate: raw.SessionUpdate,
				Content:       raw.Content,
			},
		}, nil

	case "session_died":
		var raw struct {
			SessionID  string `json:"sessionId"`
			Reason     string `json:"reason"`
			ExitCode   *int   `json:"exitCode"`
			SignalCode *int   `json:"signalCode"`
		}
		if err := json.Unmarshal(ev.Data, &raw); err != nil {
			return QwenTranslatedEvent{}, fmt.Errorf("decode session_died: %w", err)
		}
		return QwenTranslatedEvent{
			SessionDied: &QwenSessionDiedEvent{
				SessionID:  raw.SessionID,
				Reason:     raw.Reason,
				ExitCode:   raw.ExitCode,
				SignalCode: raw.SignalCode,
			},
		}, nil

	case "session_metadata_updated":
		var raw struct {
			SessionID   string `json:"sessionId"`
			DisplayName string `json:"displayName"`
		}
		if err := json.Unmarshal(ev.Data, &raw); err != nil {
			return QwenTranslatedEvent{}, fmt.Errorf("decode session_metadata_updated: %w", err)
		}
		return QwenTranslatedEvent{
			SessionMetadataUpdated: &QwenSessionMetadataUpdatedEvent{
				SessionID:   raw.SessionID,
				DisplayName: raw.DisplayName,
			},
		}, nil

	case "permission_request":
		var raw struct {
			SessionID          string            `json:"sessionId"`
			RequestID          string            `json:"requestId"`
			ToolCall           json.RawMessage   `json:"toolCall"`
			Options            []json.RawMessage `json:"options"`
			OriginatorClientID string            `json:"originatorClientId"`
		}
		if err := json.Unmarshal(ev.Data, &raw); err != nil {
			return QwenTranslatedEvent{}, fmt.Errorf("decode permission_request: %w", err)
		}
		return QwenTranslatedEvent{
			PermissionRequest: &QwenPermissionRequestEvent{
				SessionID:          raw.SessionID,
				RequestID:          raw.RequestID,
				ToolCall:           raw.ToolCall,
				Options:            raw.Options,
				OriginatorClientID: raw.OriginatorClientID,
			},
		}, nil

	case "permission_resolved":
		var raw struct {
			RequestID string          `json:"requestId"`
			Outcome   json.RawMessage `json:"outcome"`
		}
		if err := json.Unmarshal(ev.Data, &raw); err != nil {
			return QwenTranslatedEvent{}, fmt.Errorf("decode permission_resolved: %w", err)
		}
		return QwenTranslatedEvent{
			PermissionResolved: &QwenPermissionResolvedEvent{
				RequestID: raw.RequestID,
				Outcome:   raw.Outcome,
			},
		}, nil

	case "model_switched":
		var raw struct {
			SessionID string `json:"sessionId"`
			ModelID   string `json:"modelId"`
		}
		if err := json.Unmarshal(ev.Data, &raw); err != nil {
			return QwenTranslatedEvent{}, fmt.Errorf("decode model_switched: %w", err)
		}
		return QwenTranslatedEvent{
			ModelSwitched: &QwenModelSwitchedEvent{
				SessionID: raw.SessionID,
				ModelID:   raw.ModelID,
			},
		}, nil

	case "model_switch_failed":
		var raw struct {
			SessionID        string `json:"sessionId"`
			RequestedModelID string `json:"requestedModelId"`
			Error            string `json:"error"`
		}
		if err := json.Unmarshal(ev.Data, &raw); err != nil {
			return QwenTranslatedEvent{}, fmt.Errorf("decode model_switch_failed: %w", err)
		}
		return QwenTranslatedEvent{
			ModelSwitchFailed: &QwenModelSwitchFailedEvent{
				SessionID:        raw.SessionID,
				RequestedModelID: raw.RequestedModelID,
				Error:            raw.Error,
			},
		}, nil

	case "session_snapshot":
		var raw struct {
			SessionID           string `json:"sessionId"`
			CurrentModelID      string `json:"currentModelId"`
			CurrentApprovalMode string `json:"currentApprovalMode"`
		}
		if err := json.Unmarshal(ev.Data, &raw); err != nil {
			return QwenTranslatedEvent{}, fmt.Errorf("decode session_snapshot: %w", err)
		}
		return QwenTranslatedEvent{
			Snapshot: &QwenSnapshotEvent{
				SessionID:           raw.SessionID,
				CurrentModelID:      raw.CurrentModelID,
				CurrentApprovalMode: raw.CurrentApprovalMode,
			},
		}, nil

	default:
		// Unknown but well-formed: surface as Unknown so the bridge can
		// increment its counter and continue the stream.
		return QwenTranslatedEvent{
			Unknown: &QwenUnknownEvent{Type: ev.Type, Raw: ev.Data},
		}, nil
	}
}
