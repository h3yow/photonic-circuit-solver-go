// Package errors defines classified error kinds for the photonic circuit engine.
// Every user-facing failure carries a Kind so callers can distinguish rejection
// reasons (illegal topology, calibration failure, version incompatibility, ...).
package errors

import "fmt"

// Kind enumerates the distinguishable failure categories.
type Kind string

const (
	KindNone               Kind = ""
	KindModelDecode        Kind = "model_decode_error"        // JSON/模型无法解码
	KindUnsupportedVersion Kind = "unsupported_version"       // 版本超出支持范围
	KindUnknownField       Kind = "unknown_field"             // 模型出现未知字段
	KindUnknownDeviceType  Kind = "unknown_device_type"       // 未知器件类型
	KindMissingPort        Kind = "missing_port"              // 连接引用了不存在的端口
	KindPortDimension      Kind = "port_dimension_mismatch"   // 单模端口上出现多模/维度不符
	KindDanglingConnection Kind = "dangling_connection"       // 悬空连接（端口未接且未声明为外部端口）
	KindPortInUse          Kind = "port_already_connected"    // 端口被重复连接
	KindTopologyCycle      Kind = "topology_cycle"            // 成环非法拓扑
	KindInvalidParameter   Kind = "invalid_parameter"         // 器件/激励参数非法
	KindNonFiniteInput     Kind = "non_finite_input"          // 非有限数值输入
	KindOutOfPhysicalRange Kind = "out_of_physical_range"     // 超出物理边界
	KindSolverDivergence   Kind = "solver_divergence"         // 两种独立求解器结果分歧
	KindSolverIterations   Kind = "solver_iteration_limit"    // 迭代达上限未收敛
	KindCalibrationNoSolve Kind = "calibration_no_solution"   // 标定无解
	KindCalibrationRange   Kind = "calibration_out_of_range"  // 超出可达相位范围
	KindCalibrationDiverge Kind = "calibration_not_converged" // 标定迭代不收敛
	KindLimitExceeded      Kind = "limit_exceeded"            // 规模/批量/迭代超限
)

// Error is a classified engine error.
type Error struct {
	Kind    Kind
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// New builds an Error of the given kind.
func New(kind Kind, format string, args ...any) error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// Wrap builds an Error wrapping cause.
func Wrap(kind Kind, cause error, format string, args ...any) error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...), Cause: cause}
}

// KindOf extracts the Kind from an error, returning KindNone if unclassified.
func KindOf(err error) Kind {
	var e *Error
	if As(err, &e) {
		return e.Kind
	}
	return KindNone
}
