// Package solver 实现本地光子电路仿真引擎：
// 模型载入与版本兼容（1.0/1.1/1.2，含器件级损耗单位）、拓扑校验、
// 两种独立传播求解途径、移相器相位标定与漂移补偿、
// 物理量审计报告、跨版本跨声明等价核验，以及并发安全的请求处理。
//
// 并发模型：Engine 创建后只读；CompiledCircuit 不可变；
// 每次 Solve/Calibrate/Audit/CompareModels 的全部临时变量（散射块、
// 场向量、矩阵、报告结构）均在调用栈内独立分配，编译结果与缓存在
// 请求间零共享，因此多个 goroutine 并发调用同一电路得到逐位一致的结论。
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

// Audit 对已载入的电路生成物理量审计报告。
// 已编译电路通过了全部参数校验，因此报告中各器件必然可用；
// 需要体检“载不入”的模型时请用 AuditModel。
func (e *Engine) Audit(ctx context.Context, c *CompiledCircuit) (*AuditReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil {
		return nil, newError(ErrInvalidParameter, "nil compiled circuit")
	}
	return auditModel(c.model), nil
}

// AuditModel 直接对模型文档生成审计报告（只解析与迁移，不做拓扑编译）。
// 文档级问题（版本越界、未知字段、冲突声明、迁移语义不确定）导致整体失败，
// 返回 nil 报告与错误，绝不返回半截报告；
// 器件级参数问题不使整体失败，而是把对应器件标为不可用并给出原因。
func (e *Engine) AuditModel(ctx context.Context, data []byte) (*AuditReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m, err := parseModel(data, e.limits)
	if err != nil {
		return nil, err
	}
	return auditModel(m), nil
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
