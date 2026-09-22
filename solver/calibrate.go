package solver

import (
	"math"
)

// 相位标定约定（详见 docs/overview.md）：
//
//	测量相位按时间因子 e^{-iωt} 约定，拟合模型（中心化二次式）
//	    φ(u) = c0 + c1 (u-ū) + c2 (u-ū)^2
//	c1>0 且在测量区间上导数恒正 => 单调可解释、单值可逆。
//	反演在 [u_lo,u_hi] 上以二分迭代进行，迭代次数受 Limits 约束。
const (
	// calibFitTol 是拟合残差容忍度（弧度）：超出则认为样本不自洽。
	calibFitTol = 1e-6
	// calibPhaseTol 是反演残差与可达性判定的相位容忍度（弧度）。
	calibPhaseTol = 1e-10
)

// CalibPoint 是一个标定点：控制量 u 与测得相位 phi（弧度）。
type CalibPoint struct {
	Control float64
	Phase   float64
}

// CalibRequest 描述一次移相器标定（可选漂移补偿）请求。
// Samples 至少 3 点，控制量必须严格递增；Drift 给出当前时刻复测点。
type CalibRequest struct {
	ShifterID   string
	Samples     []CalibPoint
	TargetPhase float64 // 期望达到的相位（弧度，模 2π 等价）
	Drift       []CalibPoint
}

// CalibResult 是确定、可复现的标定结果。
type CalibResult struct {
	ShifterID  string
	U0         float64 // 拟合中心 ū
	K          float64 // 中心处斜率 c1
	Gamma      float64 // 二次系数 c2
	Intercept  float64 // c0 = φ(ū)
	DriftShift float64 // 估计并补偿的漂移量（弧度，已 wrap 到 (-π,π]）
	Control    float64 // 达成目标相位所需控制量
	Iterations int     // 反演迭代次数
	Residual   float64 // 拟合最大残差（弧度）
}

// quadraticFit 是中心化二次模型。
type quadraticFit struct {
	ubar float64
	c0   float64
	c1   float64
	c2   float64
	ulo  float64 // 标定覆盖区间
	uhi  float64
}

func (f quadraticFit) phi(u float64) float64 {
	x := u - f.ubar
	return f.c0 + f.c1*x + f.c2*x*x
}

// calibrate 执行单调可解释的相位标定并给出达成目标相位的控制量。
func calibrate(c *CompiledCircuit, req CalibRequest, limits Limits) (*CalibResult, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	d, err := findShifter(c, req.ShifterID)
	if err != nil {
		return nil, err
	}
	if !isFinite(req.TargetPhase) {
		return nil, newError(ErrNonFiniteInput, "target phase must be finite").
			withDetail("shifter", req.ShifterID)
	}
	samples, ulo, uhi, err := prepareSamples(req)
	if err != nil {
		return nil, err
	}

	fit, residual, err := fitMonotoneQuadratic(samples, ulo, uhi)
	if err != nil {
		return nil, err
	}
	if residual > calibFitTol {
		return nil, newError(ErrCalibNoSolution,
			"calibration samples do not admit a monotone quadratic response (max residual %g rad > %g)",
			residual, calibFitTol).
			withDetail("shifter", req.ShifterID).
			withDetail("max_residual", residual).
			withDetail("tolerance", calibFitTol).
			withDetail("sample_count", len(samples))
	}

	drift, err := estimateDrift(fit, req.Drift, req.ShifterID)
	if err != nil {
		return nil, err
	}

	// 可达相位区间（未加漂移的器件固有区间）。
	phiLo := fit.phi(ulo)
	phiHi := fit.phi(uhi)

	// 将漂移补偿后的目标映射到 [phiLo, phiLo+2π) 内的唯一代表元。
	want := req.TargetPhase - drift
	t := phiLo + wrap2Pi(want-phiLo)
	if t > phiHi+calibPhaseTol {
		return nil, newError(ErrCalibOutOfRange,
			"target phase %g (after drift %+g) is outside reachable interval [%g,%g] rad for shifter %q",
			req.TargetPhase, drift, phiLo, phiHi, req.ShifterID).
			withDetail("shifter", req.ShifterID).
			withDetail("target", req.TargetPhase).
			withDetail("drift_shift", drift).
			withDetail("reachable_lo", phiLo).
			withDetail("reachable_hi", phiHi).
			withDetail("u_range", []float64{ulo, uhi})
	}

	u, iters, err := invertMonotone(fit, t, ulo, uhi, limits.MaxCalibIterations)
	if err != nil {
		return nil, err
	}
	_ = d
	return &CalibResult{
		ShifterID:  req.ShifterID,
		U0:         fit.ubar,
		K:          fit.c1,
		Gamma:      fit.c2,
		Intercept:  fit.c0,
		DriftShift: drift,
		Control:    u,
		Iterations: iters,
		Residual:   residual,
	}, nil
}

func findShifter(c *CompiledCircuit, id string) (*compiledDevice, error) {
	idx, ok := c.deviceByID[id]
	if !ok {
		return nil, newError(ErrPortMissing, "unknown phaseshifter %q", id).
			withDetail("shifter", id)
	}
	d := &c.devices[idx]
	if d.kind != KindPhaseshifter {
		return nil, newError(ErrInvalidParameter, "device %q is %s, not a phaseshifter", id, d.kind).
			withDetail("shifter", id).withDetail("kind", d.kind)
	}
	return d, nil
}

// prepareSamples 校验有限性、最少点数与严格递增的控制量，
// 并顺序展开相位到连续分支，要求相位单调不减。
func prepareSamples(req CalibRequest) ([]CalibPoint, float64, float64, error) {
	if len(req.Samples) < 3 {
		return nil, 0, 0, newError(ErrCalibNoSolution,
			"at least 3 calibration samples are required, got %d", len(req.Samples)).
			withDetail("shifter", req.ShifterID).
			withDetail("sample_count", len(req.Samples))
	}
	out := make([]CalibPoint, 0, len(req.Samples))
	prevU := math.Inf(-1)
	prevPhi := 0.0
	for i, s := range req.Samples {
		if !isFinite(s.Control) || !isFinite(s.Phase) {
			return nil, 0, 0, newError(ErrNonFiniteInput,
				"calibration sample %d contains NaN/Inf", i).
				withDetail("shifter", req.ShifterID).
				withDetail("sample_index", i)
		}
		if i > 0 && s.Control <= prevU {
			return nil, 0, 0, newError(ErrCalibNoSolution,
				"calibration controls must be strictly increasing (index %d: %g after %g)",
				i, s.Control, prevU).
				withDetail("shifter", req.ShifterID).
				withDetail("sample_index", i).
				withDetail("control", s.Control).
				withDetail("previous_control", prevU)
		}
		phi := s.Phase
		if i > 0 {
			phi = prevPhi + wrapPi(s.Phase-wrapPi(prevPhi))
			// 展开后若相对上一点的增量为负，则响应不是单调单值映射。
			if phi < prevPhi-calibPhaseTol {
				return nil, 0, 0, newError(ErrCalibNoSolution,
					"measured phase is not monotonic at index %d (%g after %g)",
					i, phi, prevPhi).
					withDetail("shifter", req.ShifterID).
					withDetail("sample_index", i).
					withDetail("phase_unwrapped", phi).
					withDetail("previous_phase", prevPhi)
			}
		}
		out = append(out, CalibPoint{Control: s.Control, Phase: phi})
		prevU, prevPhi = s.Control, phi
	}
	return out, out[0].Control, out[len(out)-1].Control, nil
}

// fitMonotoneQuadratic 用中心化设计做最小二乘二次拟合，并强制单调性。
func fitMonotoneQuadratic(samples []CalibPoint, ulo, uhi float64) (quadraticFit, float64, error) {
	n := float64(len(samples))
	var su float64
	for _, s := range samples {
		su += s.Control
	}
	ubar := su / n

	// 正规方程（基 1, x, x^2，x=u-ū）。
	var m [3][3]float64
	var rhs [3]float64
	for _, s := range samples {
		x := s.Control - ubar
		y := s.Phase
		basis := [3]float64{1, x, x * x}
		for i := 0; i < 3; i++ {
			rhs[i] += basis[i] * y
			for j := 0; j < 3; j++ {
				m[i][j] += basis[i] * basis[j]
			}
		}
	}
	coef, err := solve3(m, rhs)
	if err != nil {
		return quadraticFit{}, 0, newError(ErrCalibNoSolution,
			"least-squares design is singular (samples are degenerate)").
			withDetail("cause", err.Error())
	}
	f := quadraticFit{ubar: ubar, c0: coef[0], c1: coef[1], c2: coef[2], ulo: ulo, uhi: uhi}

	// 单调性：导数 c1 + 2c2 x 在覆盖区间（含安全外推裕度）上必须恒为正。
	deriv := func(u float64) float64 { return f.c1 + 2*f.c2*(u-f.ubar) }
	margin := (uhi - ulo) * 1e-9
	dlo := math.Min(deriv(ulo-margin), deriv(uhi+margin))
	if !(dlo > 0) {
		return quadraticFit{}, 0, newError(ErrCalibNoSolution,
			"fitted response is not strictly monotone on the calibrated span (min derivative %g)", dlo).
			withDetail("min_derivative", dlo).
			withDetail("slope_at_center", f.c1).
			withDetail("quadratic", f.c2)
	}

	maxRes := 0.0
	for _, s := range samples {
		if r := math.Abs(f.phi(s.Control) - s.Phase); r > maxRes {
			maxRes = r
		}
	}
	return f, maxRes, nil
}

// solve3 用部分主元高斯消元解 3x3 实线性方程组。
func solve3(a [3][3]float64, b [3]float64) ([3]float64, error) {
	var x [3]float64
	aug := make([][]float64, 3)
	for i := range aug {
		aug[i] = []float64{a[i][0], a[i][1], a[i][2], b[i]}
	}
	for col := 0; col < 3; col++ {
		piv, best := col, math.Abs(aug[col][col])
		for r := col + 1; r < 3; r++ {
			if v := math.Abs(aug[r][col]); v > best {
				best, piv = v, r
			}
		}
		if best < 1e-18 {
			return x, newError(ErrCalibNoSolution, "singular normal-equation pivot at column %d", col)
		}
		aug[col], aug[piv] = aug[piv], aug[col]
		for r := col + 1; r < 3; r++ {
			f := aug[r][col] / aug[col][col]
			for k := col; k < 4; k++ {
				aug[r][k] -= f * aug[col][k]
			}
		}
	}
	for i := 2; i >= 0; i-- {
		s := aug[i][3]
		for j := i + 1; j < 3; j++ {
			s -= aug[i][j] * x[j]
		}
		x[i] = s / aug[i][i]
		if !isFinite(x[i]) {
			return x, newError(ErrNonFiniteInput, "non-finite fit coefficient")
		}
	}
	return x, nil
}

// estimateDrift 用复测点估计恒定相位漂移：各点残差（wrap 到 (-π,π]）的均值。
func estimateDrift(fit quadraticFit, drift []CalibPoint, shifterID string) (float64, error) {
	if len(drift) == 0 {
		return 0, nil
	}
	var acc float64
	for i, s := range drift {
		if !isFinite(s.Control) || !isFinite(s.Phase) {
			return 0, newError(ErrNonFiniteInput, "drift point %d contains NaN/Inf", i).
				withDetail("shifter", shifterID).
				withDetail("sample_index", i)
		}
		// 漂移点必须落在标定覆盖区间内，否则无法用拟合模型解释。
		if s.Control < fit.ulo || s.Control > fit.uhi {
			return 0, newError(ErrCalibNoSolution,
				"drift point %d at u=%g lies outside calibrated span [%g,%g]",
				i, s.Control, fit.ulo, fit.uhi).
				withDetail("shifter", shifterID).
				withDetail("sample_index", i).
				withDetail("control", s.Control).
				withDetail("span", []float64{fit.ulo, fit.uhi})
		}
		acc += wrapPi(s.Phase - fit.phi(s.Control))
	}
	delta := acc / float64(len(drift))
	if !isFinite(delta) {
		return 0, newError(ErrCalibNoSolution, "drift estimate is non-finite").
			withDetail("shifter", shifterID)
	}
	return wrapPi(delta), nil
}

// invertMonotone 在 [ulo,uhi] 上二分求解 φ(u)=target；
// 超过最大迭代次数仍不满足残差即明确报未收敛。
func invertMonotone(fit quadraticFit, target, ulo, uhi float64, maxIter int) (float64, int, error) {
	lo, hi := ulo, uhi
	flo := fit.phi(lo) - target
	fhi := fit.phi(hi) - target
	if !isFinite(flo) || !isFinite(fhi) {
		return 0, 0, newError(ErrCalibNotConverged, "non-finite residual at interval endpoints")
	}
	iters := 0
	for iters < maxIter {
		iters++
		mid := (lo + hi) / 2
		fm := fit.phi(mid) - target
		if !isFinite(fm) {
			return 0, iters, newError(ErrCalibNotConverged,
				"non-finite residual during inversion at u=%g", mid).
				withDetail("control", mid).
				withDetail("iterations", iters)
		}
		if math.Abs(fm) <= calibPhaseTol {
			return mid, iters, nil
		}
		if flo*fm <= 0 {
			hi, fhi = mid, fm
		} else {
			lo, flo = mid, fm
		}
	}
	return 0, iters, newError(ErrCalibNotConverged,
		"phase inversion did not converge within %d iterations (residual %g > %g)",
		maxIter, math.Abs((flo+fhi)/2), calibPhaseTol).
		withDetail("max_iterations", maxIter).
		withDetail("interval", []float64{ulo, uhi}).
		withDetail("target", target)
}

// wrapPi 把角度折叠到 (-π, π]。
func wrapPi(a float64) float64 {
	return wrap2Pi(a+math.Pi) - math.Pi
}

// wrap2Pi 把角度折叠到 [0, 2π)。
func wrap2Pi(a float64) float64 {
	twoPi := 2 * math.Pi
	r := math.Mod(a, twoPi)
	if r < 0 {
		r += twoPi
	}
	if r == twoPi { // 数值上来自负零边界时归入 0
		r = 0
	}
	return r
}
