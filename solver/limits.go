package solver

// Limits 集中管理电路规模、求解批量与迭代次数上限。
// 零值 Limits 不可用，请通过 DefaultLimits 获取。
type Limits struct {
	// MaxDevices 单个电路允许的最大器件数。
	MaxDevices int
	// MaxConnections 允许的最大连接条数。
	MaxConnections int
	// MaxBatchSize 单次求解请求允许的最大激励条数。
	MaxBatchSize int
	// MaxCalibIterations 单次标定允许的最大迭代次数。
	MaxCalibIterations int
	// SolverTolerance 两种求解途径交叉校验的相对+绝对容差。
	// 判定：|a-b| <= SolverTolerance*max(1,|a|,|b|)。
	SolverTolerance float64
	// EquivalenceTolerance 跨文档等价核验的相对+绝对容差，判定式同上。
	EquivalenceTolerance float64
	// MaxInputAmplitude 允许注入的最大复振幅模长（物理边界）。
	MaxInputAmplitude float64
	// MaxControl 移相器控制量的允许范围（物理边界，绝对值）。
	MaxControl float64
}

// DefaultLimits 返回确定的默认上限策略。
// 这些数值是稳定性边界而非物理常数，可通过 EngineOption 覆盖。
func DefaultLimits() Limits {
	return Limits{
		MaxDevices:         4096,
		MaxConnections:     8192,
		MaxBatchSize:       1024,
		MaxCalibIterations: 100,
		// 两条独立数值途径之间约定 1e-9 相对容差（见 docs/overview.md）。
		SolverTolerance: 1e-9,
		// 跨文档等价核验约定同一量级容差。
		EquivalenceTolerance: 1e-9,
		MaxInputAmplitude:    1e6,
		MaxControl:           1e6,
	}
}

// validate 检查 Limits 自身的一致性。
func (l Limits) validate() error {
	if l.MaxDevices <= 0 || l.MaxConnections <= 0 || l.MaxBatchSize <= 0 ||
		l.MaxCalibIterations <= 0 || l.SolverTolerance <= 0 || l.EquivalenceTolerance <= 0 ||
		l.MaxInputAmplitude <= 0 || l.MaxControl <= 0 {
		return newError(ErrInvalidParameter, "limits must contain strictly positive values").
			withDetail("limits", l)
	}
	return nil
}
