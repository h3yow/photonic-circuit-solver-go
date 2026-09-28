// Package solver 实现本地光子电路仿真引擎：
// 模型载入与版本兼容、拓扑校验、两种独立传播求解途径、
// 移相器相位标定与漂移补偿、物理量审计与跨版本等价核验，
// 以及并发安全的请求处理。
//
// 并发模型：Engine 创建后只读；CompiledCircuit 不可变；
// 每次 Solve/Calibrate/Audit/CompareModels 的全部临时变量
// （散射块、场向量、矩阵、报告）均在调用栈内独立分配，
// 编译结果与缓存在请求间零共享，
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

// Audit 对已载入（编译通过）的电路生成物理量审计报告。
// 报告逐器件列出归一化后的物理量、单位与取值来源；
// 纯函数式实现，不改动电路，可并发调用。
func (e *Engine) Audit(ctx context.Context, c *CompiledCircuit) (*AuditReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil {
		return nil, newError(ErrInvalidParameter, "nil compiled circuit")
	}
	return auditModel(c.model), nil
}

// AuditDocument 对原始模型文档生成物理量审计报告。
// 与 Audit 不同，它只要求文档可解析、可迁移（用于体检历史/变体文件）：
// 器件级参数缺失或冲突不会导致整体失败，而是把对应物理量标为不可用；
// 但文档级问题（未知字段、版本超界、迁移语义不确定、冲突声明）一律
// 整体拒绝，绝不返回半截报告。
func (e *Engine) AuditDocument(ctx context.Context, data []byte) (*AuditReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m, err := parseModel(data, e.limits)
	if err != nil {
		return nil, err
	}
	return auditModel(m), nil
}

// CompareModels 对两份模型文档做跨版本、跨声明的等价核验。
// 两份文档都必须能确定地解析、迁移，且物理语义合法（参数取值在物理范围内、
// 同一器件不得同时给出互斥声明，如分束器的 theta 与 reflectivity）；
// 任一份落在无法确定语义的版本/声明组合或物理含义无法成立的取值上，
// 整体拒绝并说明原因，绝不挑一份继续算，也绝不返回半截报告。
// 核验为纯函数：不改动任何已载入电路，可并发调用。
func (e *Engine) CompareModels(ctx context.Context, docA, docB []byte) (*EquivalenceReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ma, err := parseModel(docA, e.limits)
	if err != nil {
		return nil, undecidableEquivalence("A", err)
	}
	mb, err := parseModel(docB, e.limits)
	if err != nil {
		return nil, undecidableEquivalence("B", err)
	}
	// 物理语义校验：等价核验必须建立在取值合法、声明无歧义的模型上。
	// 复用编译期同一套规则（工作波长、逐器件参数范围、互斥声明），
	// 避免越界值（如按百分比填写的 reflectivity=30）折算为 NaN 后
	// 产出无法序列化、没有物理意义的报告；但不做拓扑/连接校验——
	// 结构差异本身正是核验要报告的内容。
	if err := validateCircuitParams(&ma.Circuit); err != nil {
		return nil, undecidableEquivalence("A", err)
	}
	if err := validateCircuitParams(&mb.Circuit); err != nil {
		return nil, undecidableEquivalence("B", err)
	}
	rep, err := compareModels(ma, mb, e.limits.EquivalenceTolerance)
	if err != nil {
		return nil, err
	}
	return rep, nil
}

// undecidableEquivalence 保留原始错误码并补充“无法确定等价语义”的上下文。
func undecidableEquivalence(side string, err error) error {
	if se, ok := AsSolverError(err); ok {
		return wrapError(se.Code, err,
			"equivalence undecidable: document %s cannot be compared with determined semantics", side)
	}
	return err
}
