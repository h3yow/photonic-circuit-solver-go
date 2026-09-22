// Package engine is the concurrency-safe, resource-bounded facade over the
// photonic circuit simulation. It owns compiled-circuit caches (immutable
// values only), enforces configurable circuit/batch/iteration limits and
// de-duplicates concurrent calibration of the same shifter so repeated
// requests never trigger repeated physical calibration.
package engine

import (
	"sync"

	"github.com/h3yow/photonic-circuit-solver-go/calibration"
	"github.com/h3yow/photonic-circuit-solver-go/model"
	"github.com/h3yow/photonic-circuit-solver-go/solver"
	"github.com/h3yow/photonic-circuit-solver-go/topology"
)

// Limits bounds circuit size, batch size and iteration effort.
type Limits struct {
	MaxDevices     int
	MaxPorts       int
	MaxConnections int
	MaxBatch       int // max solve requests in one BatchSolve call
	MaxIterations  int // solver/calibration iteration ceiling
}

// Config is the engine configuration.
type Config struct {
	Limits Limits
	Solver solver.Config
	Calib  calibration.Config
	// Policy decides behavior when a BATCH limit is exceeded:
	//   "reject"  - return KindLimitExceeded (default)
	//   "degrade" - process the oversized batch in bounded MaxBatch chunks,
	//               still validating every request (never silently wrong)
	Policy string
}

// SolveRequest is one entry of a batch; Key lets callers label requests.
type SolveRequest struct {
	Key        string
	Method     solver.Method
	CrossCheck solver.Method // empty disables cross-check
	Request    solver.Request
}

// cacheEntry holds only IMMUTABLE values: the compiled circuit and the model
// it came from. Per-request mutable state (solvers, clones) is never cached.
type cacheEntry struct {
	model    *model.CircuitModel
	compiled *topology.Compiled
}

// calibCall coalesces concurrent identical calibrations.
type calibCall struct {
	done chan struct{}
	rec  *CalibrationRecord
	err  error
}

// CalibrationRecord is the persisted outcome of a calibration.
type CalibrationRecord struct {
	CircuitKey    string
	PhaseShifter  string
	Offset        float64
	AchievedDelta float64
}

// Engine caches immutable compiled circuits and serializes calibration.
type Engine struct {
	cfg Config

	mu    sync.RWMutex
	cache map[string]*cacheEntry

	calibMu sync.Mutex
	calls   map[string]*calibCall
	records map[string]*CalibrationRecord
}

// New creates an Engine. An unknown Policy is rejected.
func New(cfg Config) (*Engine, error) { return newEngine(cfg) }
