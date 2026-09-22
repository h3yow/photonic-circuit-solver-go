package engine

import "github.com/h3yow/photonic-circuit-solver-go/model"

func cloneCircuitModel(m *model.CircuitModel) (*model.CircuitModel, error) {
	raw, err := model.Encode(m, m.Version)
	if err != nil {
		return nil, err
	}
	return model.Decode(raw)
}

// keyOf returns the content key for a cached entry.
func (e *Engine) keyOf(ce *cacheEntry) string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for k, v := range e.cache {
		if v == ce {
			return k
		}
	}
	return ""
}
