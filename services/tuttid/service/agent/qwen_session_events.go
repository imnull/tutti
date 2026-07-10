package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// SSE event consumer for `qwen serve` — opens GET /session/:id/events and
// hands each typed envelope to the caller. Per QwenLM/qwen-code docs/
// developers/daemon/09-event-schema.md, every SSE frame on this route has
// the shape { id, v: 1, type, data, originatorClientId?, _meta? }. The
// `type` field is drawn from the closed set
// DAEMON_KNOWN_EVENT_TYPE_VALUES in qwen-code's
// packages/sdk-typescript/src/daemon/events.ts, and the bridge layer that
// connects this stream to Tutti's renderer is owned by the codegen'd
// event-protocol path (see docs/adr/0001-codex-appserver-codegen-approach.md
// — the qwen adapter mirrors it).

// QwenDaemonEvent is the wire-level envelope decoded from the SSE stream.
// The Data field is kept as json.RawMessage so callers can pick the
// decoder that matches the event's `type` (per the schema, each type has
// a distinct payload shape — see 09-event-schema.md).
//
// ID is 0 for synthesized frames (slow_client_warning, replay_complete,
// client_evicted, etc.) — those are force-pushed without a per-subscriber
// event id. Handlers should treat ID=0 as "synthetic, not part of the
// replay ring".
type QwenDaemonEvent struct {
	ID                 uint64
	SchemaVersion      int
	Type               string
	Data               json.RawMessage
	OriginatorClientID string
}

// QwenEventHandler is invoked for every decoded event envelope. Returning
// a non-nil error closes the SSE stream and surfaces the error to the
// caller of SubscribeSessionEvents. Returning nil continues processing.
type QwenEventHandler func(ctx context.Context, ev QwenDaemonEvent) error

// QwenEventSubscription configures SubscribeSessionEvents.
//
// LastEventID lets the caller resume from the daemon's replay ring after
// a transient disconnect; the daemon honors Last-Event-ID on
// GET /session/:id/events and replays the missed window before live
// frames begin. Empty string starts at the head of the ring (full replay).
type QwenEventSubscription struct {
	SessionID   string
	LastEventID string
	Handler     QwenEventHandler
}

// SubscribeSessionEvents opens a long-lived SSE stream on
// GET /session/:id/events and calls sub.Handler for every decoded
// envelope until one of:
//
//   - The daemon closes the stream (EOF / TCP reset).
//   - sub.Handler returns a non-nil error.
//   - ctx is canceled.
//
// Reconnect-on-disconnect is intentionally NOT here — the caller (the
// Tutti-side bridge) owns that loop because it has the context for
// back-off, jitter, and the user-visible "session reconnecting…" UI.
func (c QwenDaemonClient) SubscribeSessionEvents(ctx context.Context, sub QwenEventSubscription) error {
	if c.Token == "" {
		return ErrQwenDaemonNotConfigured
	}
	if strings_TrimSpaceIsEmpty(sub.SessionID) {
		return errors.New("qwen session id is required")
	}
	if sub.Handler == nil {
		return errors.New("qwen event handler is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/session/"+sub.SessionID+"/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if sub.LastEventID != "" {
		req.Header.Set("Last-Event-ID", sub.LastEventID)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("qwen serve GET /session/%s/events: %w", sub.SessionID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("qwen serve /session/%s/events returned %d: %s",
			sub.SessionID, resp.StatusCode, body)
	}

	return parseQwenSSEStream(ctx, resp.Body, sub.Handler)
}

// parseQwenSSEStream walks the SSE body line-by-line per the SSE spec
// (HTML5 / RFC 8895 / WHATWG EventSource).
//
// Wire shape recap (only the fields qwen-code emits today):
//
//	id: <uint64>          // monotonic per-subscriber event id; absent on
//	                     // synthetic frames
//	event: <type>         // one of DAEMON_KNOWN_EVENT_TYPE_VALUES
//	data: <json line>     // may repeat across multiple lines; joined
//	                     // with "\n" per spec §Interpreting the Event
//	                     // Stream
//	<blank line>          // terminates the current record
//	: <comment>           // ignored
//
// Each record produces at most one QwenDaemonEvent. Lines that don't
// match a known field ("retry:" for example) are silently dropped — we
// don't honor client-side retry hints today; reconnect is the caller's
// job.
func parseQwenSSEStream(
	ctx context.Context,
	body io.Reader,
	handler QwenEventHandler,
) error {
	// 16 MiB per-line buffer — qwen-code's session_update payloads can be
	// large when ACP tool calls include full file bodies. Bigger than this
	// is almost certainly a runaway agent and should fail closed.
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var (
		idStr   string
		typeStr string
		dataBuf strings.Builder
	)
	flush := func() error {
		if dataBuf.Len() == 0 && idStr == "" && typeStr == "" {
			return nil
		}
		ev := QwenDaemonEvent{
			ID:            parseQwenEventID(idStr),
			Type:          typeStr,
			Data:          json.RawMessage(dataBuf.String()),
			SchemaVersion: 1, // qwen-code's current EVENT_SCHEMA_VERSION
		}
		dataBuf.Reset()
		idStr = ""
		typeStr = ""
		return handler(ctx, ev)
	}

	for scanner.Scan() {
		// Honor context cancellation between lines so a Ctrl-C in the
		// renderer tears down the stream promptly instead of waiting for
		// the daemon to close.
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text()
		if strings.HasPrefix(line, ":") {
			continue // SSE comment
		}
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		field, value, hasColon := strings.Cut(line, ":")
		if !hasColon {
			continue // malformed line per spec; ignore
		}
		// Per spec §Interpreting the Event Stream: if value starts with
		// a single space, strip exactly that one space (a leading space is
		// the SSE "this is data, not field name" sentinel).
		if strings.HasPrefix(value, " ") {
			value = value[1:]
		}
		switch field {
		case "id":
			idStr = value
		case "event":
			typeStr = value
		case "data":
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(value)
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read qwen SSE stream: %w", err)
	}
	// Final flush in case the daemon closed the stream without a
	// trailing blank line (rare but allowed per spec).
	return flush()
}

// parseQwenEventID parses the SSE "id:" field as a uint64. qwen-code's
// daemon uses monotonic per-subscriber integer ids; an unparseable value
// leaves ID at 0, which matches the "synthetic frame" convention and lets
// the handler down-stream treat it the same way.
func parseQwenEventID(s string) uint64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
