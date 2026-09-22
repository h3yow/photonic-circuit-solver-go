package solver

import (
	"math"
)

// SolverMethod 选择传播求解途径。
type SolverMethod string

const (
	// SolverTopological 途径一：按拓扑顺序逐器件传播。
	SolverTopological SolverMethod = "topological"
	// SolverGlobal 途径二：装配全局波变量线性方程并高斯消元。
	SolverGlobal SolverMethod = "global"
	// SolverCrossCheck 同时运行两条独立途径并按容差交叉校验（默认）。
	SolverCrossCheck SolverMethod = "crosscheck"
)

// Excitation 描述在一个外部输入端口上施加的复振幅。
type Excitation struct {
	Port      string
	Amplitude complex128
}

// SolveRequest 是一次传播求解请求。
// 对同一移相器，Controls 与 Phases 不得同时给出，二者语义不做隐式回退。
type SolveRequest struct {
	Batch    []Excitation       // 多条激励视为独立场景，逐一求解
	Controls map[string]float64 // 移相器 ID -> 控制量 u
	Phases   map[string]float64 // 移相器 ID -> 直接指定附加相位 φ（弧度）
	Method   SolverMethod
}

// PortField 是一个外部端口上的输出场。
type PortField struct {
	Port      string
	Amplitude complex128
	Power     float64 // |A|^2
}

// SolveResult 是单条激励场景的全部外部端口输出（按端口名稳定排序）。
type SolveResult struct {
	Outputs []PortField
}

// BatchResult 对应一次请求中各条激励的求解结果，顺序与请求一致。
type BatchResult struct {
	Results []*SolveResult
}

// solveSettings 是校验后每个移相器的确定设置。
type solveSettings map[string]phaseModel

// validate 校验请求并产出确定的求解设置；任何非法输入均被拒绝，
// 不返回非物理结果，不做静默截断。
func (req SolveRequest) validate(c *CompiledCircuit) (solveSettings, error) {
	lim := c.limits
	if len(req.Batch) == 0 {
		return nil, newError(ErrInvalidParameter, "solve request requires at least one excitation")
	}
	if len(req.Batch) > lim.MaxBatchSize {
		return nil, newError(ErrLimitExceeded, "batch size %d exceeds limit %d",
			len(req.Batch), lim.MaxBatchSize).
			withDetail("batch_size", len(req.Batch)).
			withDetail("limit", lim.MaxBatchSize)
	}

	knownInputs := map[string]struct{}{}
	for _, n := range c.externalInputs {
		knownInputs[n] = struct{}{}
	}
	for i, ex := range req.Batch {
		if ex.Port == "" {
			return nil, newError(ErrInvalidParameter, "excitation %d has empty port", i).
				withDetail("excitation_index", i)
		}
		if _, ok := knownInputs[ex.Port]; !ok {
			return nil, newError(ErrPortMissing, "excitation %d references undeclared input port %q",
				i, ex.Port).
				withDetail("excitation_index", i).
				withDetail("port", ex.Port)
		}
		if !cfinite(ex.Amplitude) {
			return nil, newError(ErrNonFiniteInput,
				"excitation %d on port %q is NaN/Inf", i, ex.Port).
				withDetail("excitation_index", i).
				withDetail("port", ex.Port)
		}
		if mag := cabs(ex.Amplitude); mag > lim.MaxInputAmplitude {
			return nil, newError(ErrOutOfBounds,
				"excitation %d amplitude magnitude %g exceeds physical bound %g (no silent clipping)",
				i, mag, lim.MaxInputAmplitude).
				withDetail("excitation_index", i).
				withDetail("port", ex.Port).
				withDetail("magnitude", mag).
				withDetail("bound", lim.MaxInputAmplitude)
		}
	}

	settings := solveSettings{}
	shifters := map[string]int{}
	for _, d := range c.devices {
		if d.kind == KindPhaseshifter {
			shifters[d.id] = d.idx
			settings[d.id] = phaseModel{useControl: false, phi: 0}
		}
	}
	assign := func(m map[string]float64, useControl bool) error {
		for id, v := range m {
			idx, ok := shifters[id]
			if !ok {
				return newError(ErrPortMissing, "setting references unknown phaseshifter %q", id).
					withDetail("shifter", id)
			}
			_ = idx
			if !isFinite(v) {
				return newError(ErrNonFiniteInput, "setting for phaseshifter %q is NaN/Inf", id).
					withDetail("shifter", id)
			}
			if math.Abs(v) > lim.MaxControl {
				return newError(ErrOutOfBounds,
					"setting %g for phaseshifter %q exceeds bound %g", v, id, lim.MaxControl).
					withDetail("shifter", id).
					withDetail("value", v).
					withDetail("bound", lim.MaxControl)
			}
			settings[id] = phaseModel{useControl: useControl, u: v, phi: v}
		}
		return nil
	}
	if err := assign(req.Controls, true); err != nil {
		return nil, err
	}
	for id, u := range req.Controls {
		if _, dup := req.Phases[id]; dup {
			return nil, newError(ErrInvalidParameter,
				"phaseshifter %q is given both control %g and explicit phase %g (must choose one)",
				id, u, req.Phases[id]).
				withDetail("shifter", id).
				withDetail("control", u).
				withDetail("phase", req.Phases[id])
		}
	}
	if err := assign(req.Phases, false); err != nil {
		return nil, err
	}

	switch req.Method {
	case SolverTopological, SolverGlobal, SolverCrossCheck, "":
	default:
		return nil, newError(ErrInvalidParameter, "unknown solver method %q", req.Method).
			withDetail("method", string(req.Method))
	}
	return settings, nil
}

// solve 按选定途径对整批激励逐一求解；方法为空时默认交叉校验。
func solve(c *CompiledCircuit, req SolveRequest) (*BatchResult, error) {
	settings, err := req.validate(c)
	if err != nil {
		return nil, err
	}
	method := req.Method
	if method == "" {
		method = SolverCrossCheck
	}

	br := &BatchResult{}
	for i, ex := range req.Batch {
		res, err := singleSolve(c, ex, settings, method)
		if err != nil {
			if se, ok := AsSolverError(err); ok {
				return nil, wrapError(se.Code, err,
					"batch element %d (port %q) failed", i, ex.Port)
			}
			return nil, err
		}
		br.Results = append(br.Results, res)
	}
	return br, nil
}

func cabs(z complex128) float64 { return math.Hypot(real(z), imag(z)) }

func cfinite(z complex128) bool {
	return !math.IsNaN(real(z)) && !math.IsInf(real(z), 0) &&
		!math.IsNaN(imag(z)) && !math.IsInf(imag(z), 0)
}

// withinTol 按 |a-b| <= tol*max(1,|a|,|b|) 判定两个标量是否一致。
func withinTol(a, b, tol float64) bool {
	d := math.Abs(a - b)
	scale := math.Max(1.0, math.Max(math.Abs(a), math.Abs(b)))
	return d <= tol*scale
}

// resultsWithinTol 逐端口（实部、虚部）比较两个求解结果。
func resultsWithinTol(x, y *SolveResult, tol float64) (bool, string) {
	if len(x.Outputs) != len(y.Outputs) {
		return false, "output port count differs"
	}
	for i := range x.Outputs {
		px, py := x.Outputs[i], y.Outputs[i]
		if px.Port != py.Port {
			return false, "output port ordering differs at index " + itoa(i)
		}
		if !withinTol(real(px.Amplitude), real(py.Amplitude), tol) ||
			!withinTol(imag(px.Amplitude), imag(py.Amplitude), tol) {
			return false, "port " + px.Port + " amplitude disagreement"
		}
	}
	return true, ""
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}
