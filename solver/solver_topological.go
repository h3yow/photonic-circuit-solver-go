package solver

// solveTopological 以拓扑顺序逐器件传播场（独立求解途径一）。
// 它不装配任何全局矩阵：每个器件的输出仅由其已就绪的输入边决定。
// 所有临时变量均在本次调用栈内分配，结束后自然释放，不写入编译结果。
func solveTopological(c *CompiledCircuit, ex Excitation, settings solveSettings) (*SolveResult, error) {
	blocks, err := buildBlocks(c, settings)
	if err != nil {
		return nil, err
	}

	nEdges := len(c.edgeSrc)
	edgeVals := make([]complex128, nEdges)

	// 输入向量辅助：按器件输入端口顺序收集。
	inVec := func(d *compiledDevice) []complex128 {
		v := make([]complex128, len(d.inPorts))
		for j, p := range d.inPorts {
			ref := portRef{d.idx, p}
			if src, ok := c.inputEdges[ref]; ok {
				if src == ex.Port {
					v[j] = ex.Amplitude
				}
				// 未激励的外部输入视为 0（物理真空输入，非默认兜底）
			} else if e, ok := c.incomingEdge[ref]; ok {
				v[j] = edgeVals[e]
			}
		}
		return v
	}

	outAt := make([][]complex128, len(c.devices))
	for _, di := range c.order {
		d := &c.devices[di]
		b := blocks[di]
		in := inVec(d)
		out := make([]complex128, len(d.outPorts))
		for o := range out {
			var acc complex128
			for i := range in {
				acc += b.s[o][i] * in[i]
			}
			out[o] = acc
		}
		outAt[di] = out
		for j, p := range d.outPorts {
			ref := portRef{d.idx, p}
			if e, ok := c.internalOf[ref]; ok {
				edgeVals[e] = out[j]
			}
		}
	}
	// 测试注入钩子（生产路径零值）。
	if c.testTopologicalScale != 0 {
		for i := range outAt {
			for j := range outAt[i] {
				outAt[i][j] *= c.testTopologicalScale
			}
		}
	}
	return assembleOutputs(c, outAt)
}
