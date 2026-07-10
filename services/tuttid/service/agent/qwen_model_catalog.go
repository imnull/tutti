package agent

import (
	"context"
	"log/slog"
)

// QwenCodeDaemonModelLister implements AgentModelLister against the qwen
// serve daemon's /capabilities payload.
//
// Registration (NOT YET LANDED): this lister is wired into the model
// catalog by adding a QwenCode field to CachedAgentModelCatalog and an
// entry to agentModelCatalogSpecs in services/tuttid/service/agent/
// model_catalog.go. That struct change is deferred to the next iteration
// alongside the daemon lifecycle owner; until then, the lister is
// unreachable through Tutti's normal model-catalog path but stays
// compile-clean and unit-testable.
//
// TODO(qwen): once the registration lands, mirror the codex entry:
//
//	agentprovider.QwenCode: {
//	    source: "qwen-serve",
//	    ttl:    30 * time.Second,
//	    errTTL: 5 * time.Second,
//	    lister: func(c *CachedAgentModelCatalog) AgentModelLister {
//	        if c.QwenCode != nil {
//	            return c.QwenCode
//	        }
//	        return QwenCodeDaemonModelLister{}
//	    },
//	    configuredDefaultModel:    func() string { return "" },
//	    missingDefaultDescription: "Qwen Code configured custom model",
//	},
type QwenCodeDaemonModelLister struct {
	// Client is optional; when zero-valued the lister resolves the daemon
	// URL + bearer from env on each call via NewQwenDaemonClient.
	Client QwenDaemonClient
}

// ListModels fetches /capabilities and projects caps.models[] into
// AgentModelOption rows. Daemon unreachable → returns IsFallback=true so
// the catalog layer surfaces a friendly "qwen serve not running" notice
// instead of pretending the model list is empty.
func (l QwenCodeDaemonModelLister) ListModels(ctx context.Context) (AgentModelListResult, error) {
	client := l.Client
	if client.BaseURL == "" {
		client = NewQwenDaemonClient()
	}

	if err := client.Health(ctx); err != nil {
		slog.Warn(
			"qwen daemon health probe failed; returning fallback model catalog",
			"err", err,
		)
		return AgentModelListResult{
			Models:     []AgentModelOption{},
			IsFallback: true,
		}, nil
	}

	caps, err := client.Capabilities(ctx)
	if err != nil {
		slog.Warn(
			"qwen daemon capabilities fetch failed; returning fallback model catalog",
			"err", err,
		)
		return AgentModelListResult{
			Models:     []AgentModelOption{},
			IsFallback: true,
		}, nil
	}

	return AgentModelListResult{
		Models: projectQwenCapabilitiesToModelOptions(caps),
	}, nil
}

// projectQwenCapabilitiesToModelOptions pulls the model list out of the
// /capabilities payload. qwen-code surfaces models as caps.models[] of
// {id, label?, description?} objects; the field set is conservative (no
// reasoning effort, no image-input hint) until the upstream schema
// stabilizes, after which we can populate
// DefaultReasoningEffort / SupportsImageInput directly from the same
// payload.
func projectQwenCapabilitiesToModelOptions(caps map[string]any) []AgentModelOption {
	rawModels, _ := caps["models"].([]any)
	if len(rawModels) == 0 {
		return nil
	}
	out := make([]AgentModelOption, 0, len(rawModels))
	for _, raw := range rawModels {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		label, _ := m["label"].(string)
		if label == "" {
			label = id
		}
		description, _ := m["description"].(string)
		out = append(out, AgentModelOption{
			ID:          id,
			DisplayName: label,
			Description: description,
		})
	}
	return out
}
