package solver

// solveGlobal 装配全部内部波变量的线性方程组 (I - Q) e = b，
// 再用带部分主元的高斯消元求解（独立求解途径二）。
// 该路径与拓扑传播共享的只有器件散射块数值本身；
// 装配方式、方程排序与数值算法均独立。
func solveGlobal(c *CompiledCircuit, ex Excitation, settings solveSettings) (*SolveResult, error) {
	blocks, err := buildBlocks(c, settings)
	if err != nil {
		return nil, err
	}

	n := len(c.edgeSrc)
	a := make([][]complex128, n)
	for i := range a {
		a[i] = make([]complex128, n+1)
		a[i][i] = 1 // I
	}

	// 对每条内部边 e：e = Σ_j S[srcOut][j] * input_j
	// input_j 若来自另一条边 e' => -S e'；若来自被激励外部端口 => S*A。
	for e := 0; e < n; e++ {
		src := c.edgeSrc[e]
		d := &c.devices[src.dev]
		b := blocks[src.dev]
		outIdx := indexOf(d.outPorts, src.port)
		for j, ip := range d.inPorts {
			ref := portRef{d.idx, ip}
			s := b.s[outIdx][j]
			if ep, ok := c.incomingEdge[ref]; ok {
				a[e][ep] -= s
			} else if name, ok := c.inputEdges[ref]; ok && name == ex.Port {
				a[e][n] += s * ex.Amplitude
			}
		}
	}

	edgeVals, err := gaussianSolve(a)
	if err != nil {
		return nil, err
	}

	// 由边值与外部激励重建每个器件的输出。
	outAt := make([][]complex128, len(c.devices))
	for i := range c.devices {
		d := &c.devices[i]
		b := blocks[i]
		out := make([]complex128, len(d.outPorts))
		in := make([]complex128, len(d.inPorts))
		for j, p := range d.inPorts {
			ref := portRef{d.idx, p}
			if name, ok := c.inputEdges[ref]; ok && name == ex.Port {
				in[j] = ex.Amplitude
			} else if ep, ok := c.incomingEdge[ref]; ok {
				in[j] = edgeVals[ep]
			}
		}
		for o := range out {
			var acc complex128
			for j := range in {
				acc += b.s[o][j] * in[j]
			}
			out[o] = acc
		}
		outAt[i] = out
	}
	return assembleOutputs(c, outAt)
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return -1
}

// gaussianSolve 对增广矩阵 [A|b] 执行带部分主元的复高斯消元。
// 奇异或产生非有限中间量时明确报错。
func gaussianSolve(aug [][]complex128) ([]complex128, error) {
	n := len(aug)
	// 前向消元
	for col := 0; col < n; col++ {
		pivot := col
		best := mag2(aug[col][col])
		for r := col + 1; r < n; r++ {
			if m := mag2(aug[r][col]); m > best {
				best, pivot = m, r
			}
		}
		if best < 1e-24 {
			return nil, newError(ErrSolverDivergence,
				"global system matrix is singular at column %d (pivot^2=%g)", col, best).
				withDetail("column", col).
				withDetail("pivot_squared", best)
		}
		if pivot != col {
			aug[col], aug[pivot] = aug[pivot], aug[col]
		}
		pv := aug[col][col]
		for r := col + 1; r < n; r++ {
			f := aug[r][col] / pv
			if f == 0 {
				continue
			}
			for k := col; k <= n; k++ {
				aug[r][k] -= f * aug[col][k]
			}
			if !cfinite(aug[r][n]) {
				return nil, newError(ErrNonFiniteInput,
					"non-finite value during elimination at row %d", r).
					withDetail("row", r)
			}
		}
	}
	// 回代
	x := make([]complex128, n)
	for i := n - 1; i >= 0; i-- {
		s := aug[i][n]
		for j := i + 1; j < n; j++ {
			s -= aug[i][j] * x[j]
		}
		v := s / aug[i][i]
		if !cfinite(v) {
			return nil, newError(ErrNonFiniteInput, "non-finite solution element at index %d", i).
				withDetail("index", i)
		}
		x[i] = v
	}
	return x, nil
}

func mag2(z complex128) float64 {
	return real(z)*real(z) + imag(z)*imag(z)
}
