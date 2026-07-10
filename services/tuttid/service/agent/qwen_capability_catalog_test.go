package agent

import (
	"testing"
)

// Tests for projectQwenCapabilitiesToComposerOptions (the projection that
// turns the raw /capabilities payload into the shared
// ComposerCapabilityOption vocabulary). The function is pure — given a
// map, it returns a list — so no daemon or context needed.

func TestProjectQwenCapabilitiesToComposerOptions_emptyCaps(t *testing.T) {
	fallback := []ComposerCapabilityOption{{ID: "fallback:1"}}
	got := projectQwenCapabilitiesToComposerOptions(map[string]any{}, fallback)
	if len(got) != 1 || got[0].ID != "fallback:1" {
		t.Errorf("empty caps: fallback should pass through untouched, got %+v", got)
	}
}

func TestProjectQwenCapabilitiesToComposerOptions_emitsOneRowPerFeature(t *testing.T) {
	fallback := []ComposerCapabilityOption{}
	got := projectQwenCapabilitiesToComposerOptions(map[string]any{
		"features": []any{"session_create", "session_events", "session_load"},
	}, fallback)
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d (%+v)", len(got), got)
	}
	for _, row := range got {
		if row.Kind != "qwen-feature" {
			t.Errorf("row %q: Kind = %q, want qwen-feature", row.Name, row.Kind)
		}
		if row.Status != "available" {
			t.Errorf("row %q: Status = %q, want available", row.Name, row.Status)
		}
		if row.ID != "qwen-feature:"+row.Name {
			t.Errorf("row %q: ID = %q, want qwen-feature:%s", row.Name, row.ID, row.Name)
		}
		if row.Invocation != "none" {
			t.Errorf("row %q: Invocation = %q, want none", row.Name, row.Invocation)
		}
	}
}

func TestProjectQwenCapabilitiesToComposerOptions_skipsNonStringFeatures(t *testing.T) {
	// The daemon's /capabilities returns `features` as a list of strings,
	// but a defensive decoder must skip junk rather than panic. The
	// projection runs AFTER json.Decode, so anything that decoded as
	// non-string is intentional data we should ignore.
	got := projectQwenCapabilitiesToComposerOptions(map[string]any{
		"features": []any{"session_create", 42, nil, "", "session_events"},
	}, nil)
	if len(got) != 2 {
		t.Errorf("non-string features should be skipped, got %+v", got)
	}
}

func TestProjectQwenCapabilitiesToComposerOptions_preservesFallbackOrder(t *testing.T) {
	// Fallback rows come first (existing skills / surface), new qwen
	// rows append at the end so the renderer can group them separately.
	fallback := []ComposerCapabilityOption{
		{ID: "skill:foo", Kind: "skill"},
		{ID: "skill:bar", Kind: "skill"},
	}
	got := projectQwenCapabilitiesToComposerOptions(map[string]any{
		"features": []any{"session_create"},
	}, fallback)
	if len(got) != 3 {
		t.Fatalf("want 3 (2 fallback + 1 qwen), got %d", len(got))
	}
	if got[0].ID != "skill:foo" || got[1].ID != "skill:bar" {
		t.Errorf("fallback ordering broken: %+v", got[:2])
	}
	if got[2].ID != "qwen-feature:session_create" {
		t.Errorf("qwen row position: got[2].ID = %q", got[2].ID)
	}
}

func TestProjectQwenCapabilitiesToComposerOptions_missingFeatures(t *testing.T) {
	// Older daemons may not advertise `features` at all (it landed in a
	// later stage of the capability registry). When the key is absent
	// the projection returns just the fallback — never panics.
	fallback := []ComposerCapabilityOption{{ID: "skill:foo"}}
	got := projectQwenCapabilitiesToComposerOptions(map[string]any{
		"workspaceCwd": "/abs/path",
	}, fallback)
	if len(got) != 1 {
		t.Errorf("missing features: want 1 (fallback only), got %d", len(got))
	}
}
