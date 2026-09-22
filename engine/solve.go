package engine

import (
	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/solver"
)

// newSolver builds a PRIVATE solver per request. Solvers hold no shared mutable
// state (the compiled circuit is read-only), so concurrent solves cannot
// observe half-updates or drift each other's caches.
func (e *Engine) newSolver(ce *cacheEntry) (*solver.Solver, error) {
	cfg := e.cfg.Solver
	cfg.MaxIterations = e.effectiveIter(cfg.MaxIterations)
	return solver.New(ce.compiled, cfg)
}

func (e *Engine) solveOne(key string, sr SolveRequest) (*solver.Result, error) {
	ce, err := e.entry(key)
	if err != nil {
		return nil, err
	}
	sv, err := e.newSolver(ce)
	if err != nil {
		return nil, err
	}
	if sr.CrossCheck != "" {
		return sv.SolveCrossChecked(sr.methodOr(solver.MethodLinear), sr.CrossCheck, sr.Request)
	}
	return sv.Solve(sr.methodOr(solver.MethodLinear), sr.Request)
}

func (sr SolveRequest) methodOr(def solver.Method) solver.Method {
	if sr.Method == "" {
		return def
	}
	return sr.Method
}

// batchSolve validates the batch against the configured bound. Under "reject"
// an oversized batch is refused entirely; under "degrade" it is processed in
// bounded chunks. Any single failure aborts the batch and returns NO partial
// results.
func (e *Engine) batchSolve(key string, reqs []SolveRequest) ([]*solver.Result, error) {
	ce, err := e.entry(key)
	if err != nil {
		return nil, err
	}
	if len(reqs) == 0 {
		return nil, engerrors.New(engerrors.KindInvalidParameter, "empty solve batch")
	}
	chunk := e.cfg.Limits.MaxBatch
	if len(reqs) > chunk {
		if e.cfg.Policy == "reject" {
			return nil, engerrors.New(engerrors.KindLimitExceeded,
				"batch size %d exceeds limit %d (policy=reject)", len(reqs), chunk)
		}
		// degrade: process sequentially in bounded chunks.
	}
	out := make([]*solver.Result, len(reqs))
	for start := 0; start < len(reqs); start += chunk {
		end := start + chunk
		if end > len(reqs) {
			end = len(reqs)
		}
		for i := start; i < end; i++ {
			sv, err := e.newSolver(ce)
			if err != nil {
				return nil, err
			}
			sr := reqs[i]
			var res *solver.Result
			if sr.CrossCheck != "" {
				res, err = sv.SolveCrossChecked(sr.methodOr(solver.MethodLinear), sr.CrossCheck, sr.Request)
			} else {
				res, err = sv.Solve(sr.methodOr(solver.MethodLinear), sr.Request)
			}
			if err != nil {
				// Discard everything computed so far: no partial results.
				return nil, err
			}
			out[i] = res
		}
	}
	return out, nil
}
