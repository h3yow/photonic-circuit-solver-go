package solver

import (
	"math"
	"math/cmplx"
)

// 光场约定（详见 docs/overview.md）：
//   复场振幅 A 满足时间因子 e^{-iωt}；传播相位 +βL 体现为 e^{+iβL}。
//   器件散射关系 out = S · in，S 的行列顺序与 compiledDevice 的
//   inPorts/outPorts 顺序严格一致。

// deviceBlock 是一个器件在给定控制/相位设置下的数值散射块。
type deviceBlock struct {
	ins  []string
	outs []string
	s    [][]complex128 // s[outIdx][inIdx]
}

// phaseModel 描述请求对某个移相器施加的设置：
// 恰好一种来源（直接相位或控制量），不做缺省回退。
type phaseModel struct {
	useControl bool
	u          float64
	phi        float64
}

// deviceBlockFor 按器件类型构建散射块；wgPhasePerLen 已把波长/损耗折算为每微米相位与衰减。
func deviceBlockFor(d *compiledDevice, set phaseModel, wl float64) (deviceBlock, error) {
	b := deviceBlock{ins: d.inPorts, outs: d.outPorts}
	switch d.kind {
	case KindWaveguide:
		length := d.params["length"]
		neff := d.params["neff"]
		loss := d.params["loss"]
		// 内部统一单位：长度与波长均为微米。
		// β = 2π n_eff / λ_μm。编译期已把 loss 归一化为 dB/m（见 lossToDBPerM），
		// 这里先换算为 dB/μm（÷1e6），
		// 再换算为奈培/μm：α = ln(10)/10 * loss_dB_per_um。
		lossDBPerUM := loss / 1e6
		alphaPerUM := (math.Ln10 / 10.0) * lossDBPerUM
		amp := math.Exp(-alphaPerUM * length)
		phase := 2 * math.Pi * neff * length / wl
		b.s = [][]complex128{{complex(amp, 0) * cmplx.Exp(complex(0, phase))}}
	case KindPhaseshifter:
		phi, err := shifterPhase(d, set)
		if err != nil {
			return deviceBlock{}, err
		}
		b.s = [][]complex128{{cmplx.Exp(complex(0, phi))}}
	case KindBeamsplitter:
		theta := math.Pi / 4 // 缺省：50:50 对称分束器
		if t, ok := d.params["theta"]; ok {
			theta = t
		}
		if r, ok := d.params["reflectivity"]; ok {
			theta = math.Asin(math.Sqrt(r))
		}
		c := math.Cos(theta)
		s := math.Sin(theta)
		// 实 Hadamard 约定（行：c,d；列：a,b）
		b.s = [][]complex128{
			{complex(c, 0), complex(s, 0)},
			{complex(s, 0), complex(-c, 0)},
		}
	case KindResonator:
		r1 := d.params["r_self"]
		r2 := d.params["r_cross"]
		a := d.params["a"]
		phi := d.params["phi_roundtrip"]
		ejp := cmplx.Exp(complex(0, phi))
		k12 := math.Sqrt((1 - r1*r1) * (1 - r2*r2))
		denom := complex(1, 0) - complex(r1*r2*a, 0)*ejp
		// Add-drop 腔解析散射块（列：in,add；行：through,drop）。
		// 腔内往返场已解析消除，因此连接图保持无环。
		sThIn := (complex(r1, 0) - complex(r2*a, 0)*ejp) / denom
		sThAdd := complex(-k12*a, 0) * ejp / denom
		sDrIn := complex(-k12*a, 0) / denom
		sDrAdd := (complex(r2, 0) - complex(r1*a, 0)*ejp) / denom
		b.s = [][]complex128{
			{sThIn, sThAdd},
			{sDrIn, sDrAdd},
		}
	}
	return b, nil
}

// shifterPhase 计算移相器在给定设置下的相位。
// 直接相位模式：phi = phi0 + φgiven。
// 控制量模式：phi = phi0 + k (u-u0) + γ (u-u0)^2。
func shifterPhase(d *compiledDevice, set phaseModel) (float64, error) {
	phi0 := d.params["phi0"] // 缺省 0（建模期对缺失参数按缺省登记，见 buildDeviceDefaults）
	if set.useControl {
		u0 := d.params["u0"]
		k := d.params["k"]
		gamma := d.params["gamma"]
		du := set.u - u0
		phi := phi0 + k*du + gamma*du*du
		if !isFinite(phi) {
			return 0, newError(ErrNonFiniteInput, "non-finite phase produced for shifter %q", d.id).
				withDetail("shifter", d.id).withDetail("control", set.u)
		}
		return phi, nil
	}
	phi := phi0 + set.phi
	if !isFinite(phi) {
		return 0, newError(ErrNonFiniteInput, "non-finite phase produced for shifter %q", d.id).
			withDetail("shifter", d.id)
	}
	return phi, nil
}
