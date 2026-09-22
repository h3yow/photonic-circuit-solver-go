package engine

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/h3yow/photonic-circuit-solver-go/calibration"
	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/model"
	"github.com/h3yow/photonic-circuit-solver-go/solver"
	"github.com/h3yow/photonic-circuit-solver-go/topology"
)

func defaultConfig() Config {
	return Config{
		Limits: Limits{
			MaxDevices:     4096,
			MaxPorts:       8192,
			MaxConnections: 8192,
			MaxBatch:       1024,
			MaxIterations:  10000,
		},
		Solver: solver.DefaultConfig(),
		Calib:  calibration.DefaultConfig(),
		Policy: "reject",
	}
}

func newEngine(cfg Config) (*Engine, error) {
	d := defaultConfig()
	if cfg.Limits.MaxDevices <= 0 {
		cfg.Limits = d.Limits
	}
	if cfg.Solver.MaxIterations <= 0 {
		cfg.Solver = d.Solver
	}
	if cfg.Calib.MaxIterations <= 0 {
		cfg.Calib = d.Calib
	}
	if cfg.Policy == "" {
		cfg.Policy = d.Policy
	}
	if cfg.Policy != "reject" && cfg.Policy != "degrade" {
		return nil, engerrors.New(engerrors.KindInvalidParameter,
			"unknown limit policy %q (want 'reject' or 'degrade')", cfg.Policy)
	}
	return &Engine{
		cfg:     cfg,
		cache:   map[string]*cacheEntry{},
		calls:   map[string]*calibCall{},
		records: map[string]*CalibrationRecord{},
	}, nil
}

func contentKey(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// checkSize enforces circuit-size limits before expensive work.
func (e *Engine) checkSize(m *model.CircuitModel) error {
	l := e.cfg.Limits
	if len(m.Devices) > l.MaxDevices {
		return engerrors.New(engerrors.KindLimitExceeded,
			"device count %d exceeds limit %d", len(m.Devices), l.MaxDevices)
	}
	if len(m.Connections) > l.MaxConnections {
		return engerrors.New(engerrors.KindLimitExceeded,
			"connection count %d exceeds limit %d", len(m.Connections), l.MaxConnections)
	}
	ports := 0
	for i := range m.Devices {
		ports += model.PortCount(m.Devices[i].Type)
	}
	if ports > l.MaxPorts {
		return engerrors.New(engerrors.KindLimitExceeded,
			"port count %d exceeds limit %d", ports, l.MaxPorts)
	}
	return nil
}

func (e *Engine) load(raw []byte) (string, error) {
	key := contentKey(raw)
	e.mu.RLock()
	if _, ok := e.cache[key]; ok {
		e.mu.RUnlock()
		return key, nil
	}
	e.mu.RUnlock()

	m, err := model.Decode(raw)
	if err != nil {
		return "", err
	}
	if err := e.checkSize(m); err != nil {
		return "", err
	}
	comp, err := topology.Compile(m)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if old, ok := e.cache[key]; ok {
		_ = old // identical content: keep first immutable entry
		return key, nil
	}
	e.cache[key] = &cacheEntry{model: m, compiled: comp}
	return key, nil
}

func (e *Engine) entry(key string) (*cacheEntry, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ce, ok := e.cache[key]
	if !ok {
		return nil, engerrors.New(engerrors.KindMissingPort, "no compiled circuit for key %s", key)
	}
	return ce, nil
}

// effectiveIter returns the iteration budget honoring both solver config and
// the engine-wide hard ceiling.
func (e *Engine) effectiveIter(want int) int {
	if want <= 0 || want > e.cfg.Limits.MaxIterations {
		return e.cfg.Limits.MaxIterations
	}
	return want
}
