package solver

import (
	"math/cmplx"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
)

// solveComplexLinear solves A*x = b by Gaussian elimination with partial
// pivoting. A and b are not modified; a copy is used.
func solveComplexLinear(a [][]complex128, b []complex128) ([]complex128, error) {
	n := len(b)
	m := make([][]complex128, n)
	for i := range m {
		m[i] = append([]complex128(nil), a[i]...)
	}
	x := append([]complex128(nil), b...)

	for col := 0; col < n; col++ {
		pivot := col
		best := cmplx.Abs(m[col][col])
		for r := col + 1; r < n; r++ {
			if v := cmplx.Abs(m[r][col]); v > best {
				best, pivot = v, r
			}
		}
		if best < 1e-14 {
			return nil, engerrors.New(engerrors.KindInvalidParameter,
				"global scattering matrix is singular at column %d (pivot %.3e)", col, best)
		}
		if pivot != col {
			m[col], m[pivot] = m[pivot], m[col]
			x[col], x[pivot] = x[pivot], x[col]
		}
		pv := m[col][col]
		for r := col + 1; r < n; r++ {
			f := m[r][col] / pv
			if f == 0 {
				continue
			}
			m[r][col] = 0
			for k := col + 1; k < n; k++ {
				m[r][k] -= f * m[col][k]
			}
			x[r] -= f * x[col]
		}
	}
	for i := n - 1; i >= 0; i-- {
		for j := i + 1; j < n; j++ {
			x[i] -= m[i][j] * x[j]
		}
		x[i] /= m[i][i]
	}
	return x, nil
}
