package engine

import (
	"github.com/h3yow/photonic-circuit-solver-go/model"
	"github.com/h3yow/photonic-circuit-solver-go/solver"
	"github.com/h3yow/photonic-circuit-solver-go/topology"
)

// SolveOneWithOffsets solves on a PRIVATE compiled snapshot where the given
// phase shifter offsets are applied. The shared cached circuit stays
// untouched, so concurrent callers cannot influence each other.
func (e *Engine) SolveOneWithOffsets(key string, offsets map[string]float64, sr SolveRequest) (*solver.Result, error) {
	return e.solveWithOffsets(key, offsets, sr)
}

// BatchSolveWithOffsets is the batch variant.
func (e *Engine) BatchSolveWithOffsets(key string, offsets map[string]float64, reqs []SolveRequest) ([]*solver.Result, error) {
	ce, err := e.entry(key)
	if err != nil {
		return nil, err
	}
	comp, err := e.compileWithOffsets(ce, offsets)
	if err != nil {
		return nil, err
	}
	if len(reqs) == 0 {
		return nil, nil
	}
	chunk := e.cfg.Limits.MaxBatch
	if len(reqs) > chunk && e.cfg.Policy == "reject" {
		return nil, errBatchTooLarge(len(reqs), chunk)
	}
	out := make([]*solver.Result, len(reqs))
	cfg := e.cfg.Solver
	cfg.MaxIterations = e.effectiveIter(cfg.MaxIterations)
	sv, err := solver.New(comp, cfg)
	if err != nil {
		return nil, err
	}
	for i := range reqs {
		sr := reqs[i]
		var res *solver.Result
		if sr.CrossCheck != "" {
			res, err = sv.SolveCrossChecked(sr.methodOr(solver.MethodLinear), sr.CrossCheck, sr.Request)
		} else {
			res, err = sv.Solve(sr.methodOr(solver.MethodLinear), sr.Request)
		}
		if err != nil {
			return nil, err
		}
		out[i] = res
	}
	return out, nil
}

func (e *Engine) solveWithOffsets(key string, offsets map[string]float64, sr SolveRequest) (*solver.Result, error) {
	ce, err := e.entry(key)
	if err != nil {
		return nil, err
	}
	comp, err := e.compileWithOffsets(ce, offsets)
	if err != nil {
		return nil, err
	}
	cfg := e.cfg.Solver
	cfg.MaxIterations = e.effectiveIter(cfg.MaxIterations)
	sv, err := solver.New(comp, cfg)
	if err != nil {
		return nil, err
	}
	if sr.CrossCheck != "" {
		return sv.SolveCrossChecked(sr.methodOr(solver.MethodLinear), sr.CrossCheck, sr.Request)
	}
	return sv.Solve(sr.methodOr(solver.MethodLinear), sr.Request)
}

func (e *Engine) compileWithOffsets(ce *cacheEntry, offsets map[string]float64) (*topology.Compiled, error) {
	if len(offsets) == 0 {
		return ce.compiled, nil
	}
	clone, err := cloneCircuitModel(ce.model)
	if err != nil {
		return nil, err
	}
	index := map[string]int{}
	for i := range clone.Devices {
		index[clone.Devices[i].ID] = i
	}
	for id, off := range offsets {
		i, ok := index[id]
		if !ok {
			return nil, e.missingShifter(id)
		}
		if clone.Devices[i].Type != model.DevicePhaseShifter {
			return nil, e.notAShifter(id, clone.Devices[i].Type)
		}
		clone.Devices[i].Params.Offset = off
	}
	return topology.Compile(clone)
}
