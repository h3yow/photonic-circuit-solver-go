package solver

import (
	"math"
	"sort"
)

// singleSolve 对一条激励按指定途径求解，并保证输出结果非有限时拒绝。
func singleSolve(c *CompiledCircuit, ex Excitation, settings solveSettings, method SolverMethod) (*SolveResult, error) {
	switch method {
	case SolverTopological:
		return solveTopological(c, ex, settings)
	case SolverGlobal:
		return solveGlobal(c, ex, settings)
	case SolverCrossCheck:
		return crossCheck(c, ex, settings)
	default:
		return nil, newError(ErrInvalidParameter, "unknown solver method %q", method)
	}
}

// buildBlocks 在请求内为每个器件构建散射块（不缓存到编译结果上）。
func buildBlocks(c *CompiledCircuit, settings solveSettings) ([]deviceBlock, error) {
	wl := c.model.Circuit.Wavelength
	blocks := make([]deviceBlock, len(c.devices))
	for i := range c.devices {
		d := &c.devices[i]
		set := phaseModel{useControl: false, phi: 0}
		if d.kind == KindPhaseshifter {
			set = settings[d.id]
		}
		b, err := deviceBlockFor(d, set, wl)
		if err != nil {
			return nil, err
		}
		blocks[i] = b
	}
	return blocks, nil
}

// assembleOutputs 从器件输出端口振幅向量收集外部输出，按端口名稳定排序。
// outAt[deviceIdx][outPortIdx] 为该输出端口的复振幅。
func assembleOutputs(c *CompiledCircuit, outAt [][]complex128) (*SolveResult, error) {
	res := &SolveResult{}
	for _, d := range c.devices {
		for j, p := range d.outPorts {
			name, ok := c.outputEdges[portRef{d.idx, p}]
			if !ok {
				continue
			}
			a := outAt[d.idx][j]
			if !cfinite(a) {
				return nil, newError(ErrNonFiniteInput,
					"non-finite output produced at external port %q", name).
					withDetail("port", name)
			}
			pow := math.Pow(cabs(a), 2)
			res.Outputs = append(res.Outputs, PortField{
				Port:      name,
				Amplitude: a,
				Power:     pow,
			})
		}
	}
	sort.Slice(res.Outputs, func(i, j int) bool { return res.Outputs[i].Port < res.Outputs[j].Port })
	return res, nil
}

// crossCheck 运行两条独立途径，超差时明确报出分歧并附判定依据。
func crossCheck(c *CompiledCircuit, ex Excitation, settings solveSettings) (*SolveResult, error) {
	r1, err := solveTopological(c, ex, settings)
	if err != nil {
		return nil, err
	}
	r2, err := solveGlobal(c, ex, settings)
	if err != nil {
		return nil, err
	}
	tol := c.limits.SolverTolerance
	if ok, reason := resultsWithinTol(r1, r2, tol); !ok {
		se := newError(ErrSolverDivergence,
			"independent solvers disagree beyond tolerance %g: %s", tol, reason)
		se.Detail = map[string]any{}
		return nil, se.
			withDetail("tolerance", tol).
			withDetail("reason", reason).
			withDetail("input_port", ex.Port).
			withDetail("input_amplitude", []float64{real(ex.Amplitude), imag(ex.Amplitude)}).
			withDetail("topological_result", summarizeResult(r1)).
			withDetail("global_result", summarizeResult(r2))
	}
	// 一致时以拓扑途径结果为准（二者差异已在容差内）。
	return r1, nil
}

// summarizeResult 把结果压缩为可打印的判定依据（端口->[re,im,power]）。
func summarizeResult(r *SolveResult) map[string][]float64 {
	out := map[string][]float64{}
	for _, p := range r.Outputs {
		out[p.Port] = []float64{real(p.Amplitude), imag(p.Amplitude), p.Power}
	}
	return out
}
