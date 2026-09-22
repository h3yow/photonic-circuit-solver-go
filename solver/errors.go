package solver

import (
	"errors"
	"fmt"
)

// ErrorCode 是所有引擎错误的稳定分类标识，调用方可据此区分拒绝原因。
type ErrorCode string

const (
	// ErrVersionUnsupported 模型版本不在支持范围内。
	ErrVersionUnsupported ErrorCode = "VERSION_UNSUPPORTED"
	// ErrUnknownField 模型包含未知字段（严格模式下拒绝）。
	ErrUnknownField ErrorCode = "UNKNOWN_FIELD"
	// ErrModelMigration 旧版本迁移时出现无法确定缺省语义的演进。
	ErrModelMigration ErrorCode = "MODEL_MIGRATION"
	// ErrDuplicateID 器件或外部端口标识重复。
	ErrDuplicateID ErrorCode = "DUPLICATE_ID"
	// ErrPortMissing 连接引用了不存在的器件/端口。
	ErrPortMissing ErrorCode = "PORT_MISSING"
	// ErrPortDimensionMismatch 器件端口数与连接维度不一致。
	ErrPortDimensionMismatch ErrorCode = "PORT_DIMENSION_MISMATCH"
	// ErrDanglingConnection 连接悬空或外部端口未声明/多接。
	ErrDanglingConnection ErrorCode = "DANGLING_CONNECTION"
	// ErrTopologyCycle 器件连接图存在有向成环（谐振腔允许的反馈环之外的非法环）。
	ErrTopologyCycle ErrorCode = "TOPOLOGY_CYCLE"
	// ErrInvalidParameter 器件参数缺失或取值非法。
	ErrInvalidParameter ErrorCode = "INVALID_PARAMETER"
	// ErrNonFiniteInput 激励或标定数据为 NaN/Inf。
	ErrNonFiniteInput ErrorCode = "NON_FINITE_INPUT"
	// ErrOutOfBounds 输入超出物理边界（幅度、相位、控制量等）。
	ErrOutOfBounds ErrorCode = "OUT_OF_BOUNDS"
	// ErrCalibNoSolution 标定方程无解。
	ErrCalibNoSolution ErrorCode = "CALIB_NO_SOLUTION"
	// ErrCalibOutOfRange 目标相位超出移相器可达相位范围。
	ErrCalibOutOfRange ErrorCode = "CALIB_OUT_OF_RANGE"
	// ErrCalibNotConverged 标定迭代未在限定次数内收敛或发散。
	ErrCalibNotConverged ErrorCode = "CALIB_NOT_CONVERGED"
	// ErrSolverDivergence 两种独立求解途径的结果超出约定容差。
	ErrSolverDivergence ErrorCode = "SOLVER_DIVERGENCE"
	// ErrLimitExceeded 电路规模、批量或迭代次数超过配置上限。
	ErrLimitExceeded ErrorCode = "LIMIT_EXCEEDED"
)

// SolverError 携带稳定错误码与可解释的定位细节。
// Detail 中存放输入片段与判定依据，便于测试断言失败时打印。
type SolverError struct {
	Code    ErrorCode
	Message string
	Detail  map[string]any
	Cause   error
}

func (e *SolverError) Error() string {
	msg := string(e.Code) + ": " + e.Message
	if e.Cause != nil {
		msg += " (" + e.Cause.Error() + ")"
	}
	return msg
}

func (e *SolverError) Unwrap() error { return e.Cause }

// newError 构造带细节的分类错误。
func newError(code ErrorCode, format string, args ...any) *SolverError {
	return &SolverError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// withDetail 为错误附加判定依据细节，返回原错误。
func (e *SolverError) withDetail(k string, v any) *SolverError {
	if e.Detail == nil {
		e.Detail = map[string]any{}
	}
	e.Detail[k] = v
	return e
}

// wrapError 包装下层错误并保留分类与判定细节。
func wrapError(code ErrorCode, cause error, format string, args ...any) *SolverError {
	se := &SolverError{Code: code, Message: fmt.Sprintf(format, args...), Cause: cause}
	if inner, ok := AsSolverError(cause); ok && inner.Detail != nil {
		se.Detail = inner.Detail // 透传判定依据，避免包装时丢失证据
	}
	return se
}

// AsSolverError 从错误链中提取 *SolverError。
func AsSolverError(err error) (*SolverError, bool) {
	var se *SolverError
	if errors.As(err, &se) {
		return se, true
	}
	return nil, false
}

// IsCode 报告错误链中的分类错误是否匹配给定错误码。
func IsCode(err error, code ErrorCode) bool {
	se, ok := AsSolverError(err)
	if !ok {
		return false
	}
	return se.Code == code
}
