// Package solver 实现本地光子电路仿真引擎：
// 模型载入与版本兼容、拓扑校验、两种独立传播求解途径、
// 移相器相位标定与漂移补偿，以及并发安全的请求处理。
//
// 并发模型：Engine 创建后只读；CompiledCircuit 不可变；
// 每次 Solve/Calibrate 的全部临时变量（散射块、场向量、矩阵）
// 均在调用栈内独立分配，编译结果与缓存在请求间零共享，
// 因此多个 goroutine 并发调用同一电路得到逐位一致的结论。
package solver

import (
	"context"
)

// Engine 是无状态门面。
type Engine struct {
	limits Limits
}

// Option 配置引擎。
type Option func(*Engine)

// WithLimits 覆盖默认规模/迭代上限（启动时一次性校验）。
func WithLimits(l Limits) Option {
	return func(e *Engine) { e.limits = l }
}

// NewEngine 以给定选项创建引擎。
func NewEngine(opts ...Option) (*Engine, error) {
	e := &Engine{limits: DefaultLimits()}
	for _, o := range opts {
		o(e)
	}
	if err := e.limits.validate(); err != nil {
		return nil, err
	}
	return e, nil
}

// LoadModel 严格解析并编译电路模型。
// 返回的 *CompiledCircuit 不可变，可跨请求长期复用。
func (e *Engine) LoadModel(ctx context.Context, data []byte) (*CompiledCircuit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m, err := parseModel(data, e.limits)
	if err != nil {
		return nil, err
	}
	c, err := compile(m, e.limits)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// Solve 按所选途径求解探测端输出。
func (e *Engine) Solve(ctx context.Context, c *CompiledCircuit, req SolveRequest) (*BatchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil {
		return nil, newError(ErrInvalidParameter, "nil compiled circuit")
	}
	return solve(c, req)
}

// Calibrate 对移相器执行标定与漂移补偿。
// 标定过程完全由请求数据决定（固定初始界、确定性迭代），无随机状态。
func (e *Engine) Calibrate(ctx context.Context, c *CompiledCircuit, req CalibRequest) (*CalibResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil {
		return nil, newError(ErrInvalidParameter, "nil compiled circuit")
	}
	return calibrate(c, req, e.limits)
}
