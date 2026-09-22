package solver

import (
	"context"
	"testing"
)

// assertCode 校验错误被归类为期望错误码，失败时打印输入模型与判定依据。
func assertCode(t *testing.T, doc string, want ErrorCode) {
	t.Helper()
	e, _ := NewEngine()
	_, err := e.LoadModel(context.Background(), []byte(doc))
	if err == nil {
		t.Fatalf("expected error %s but model accepted.\ninput model:\n%s", want, doc)
	}
	se, ok := AsSolverError(err)
	if !ok {
		t.Fatalf("error is not *SolverError: %v\ninput model:\n%s", err, doc)
	}
	if se.Code != want {
		t.Fatalf("error code mismatch.\nwant: %s\ngot:  %s (%s)\ndetail: %#v\ninput model:\n%s",
			want, se.Code, se.Message, se.Detail, doc)
	}
	t.Logf("correctly rejected [%s]: %s (detail=%v)", want, se.Message, se.Detail)
}

func TestRejectUnknownPort(t *testing.T) {
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [{"id": "w", "kind": "waveguide", "params": {"length": 1, "neff": 2}}],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "w", "port": "bogus"}},
	      {"from": {"device": "w", "port": "out"}, "to": {"external": "out0"}}
	    ]
	  }
	}`
	assertCode(t, doc, ErrPortMissing)
}

func TestRejectUnknownDeviceInConnection(t *testing.T) {
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [{"id": "w", "kind": "waveguide", "params": {"length": 1, "neff": 2}}],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "ghost", "port": "in"}},
	      {"from": {"device": "w", "port": "out"}, "to": {"external": "out0"}}
	    ]
	  }
	}`
	assertCode(t, doc, ErrPortMissing)
}

func TestRejectPortDimensionMismatch(t *testing.T) {
	// 单模输出端口被两条连接占用（扇出）。
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [
	      {"id": "w1", "kind": "waveguide", "params": {"length": 1, "neff": 2}},
	      {"id": "w2", "kind": "waveguide", "params": {"length": 1, "neff": 2}}
	    ],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "w1", "port": "in"}},
	      {"from": {"device": "w1", "port": "out"}, "to": {"device": "w2", "port": "in"}},
	      {"from": {"device": "w1", "port": "out"}, "to": {"external": "out1"}},
	      {"from": {"device": "w2", "port": "out"}, "to": {"external": "out0"}}
	    ]
	  }
	}`
	assertCode(t, doc, ErrPortDimensionMismatch)
}

func TestRejectDirectionMismatch(t *testing.T) {
	// 把输入端口接到了连接的 from 侧。
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [
	      {"id": "b", "kind": "beamsplitter", "params": {}},
	      {"id": "w", "kind": "waveguide", "params": {"length": 1, "neff": 2}}
	    ],
	    "connects": [
	      {"from": {"device": "b", "port": "a"}, "to": {"external": "in0"}},
	      {"from": {"external": "in1"}, "to": {"device": "b", "port": "b"}},
	      {"from": {"external": "in2"}, "to": {"device": "w", "port": "in"}},
	      {"from": {"device": "b", "port": "c"}, "to": {"external": "o0"}},
	      {"from": {"device": "b", "port": "d"}, "to": {"external": "o1"}},
	      {"from": {"device": "w", "port": "out"}, "to": {"external": "o2"}}
	    ]
	  }
	}`
	assertCode(t, doc, ErrPortDimensionMismatch)
}

func TestRejectDangling(t *testing.T) {
	// bs 输出端口 d 悬空。
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [{"id": "b", "kind": "beamsplitter", "params": {}}],
	    "connects": [
	      {"from": {"external": "a"}, "to": {"device": "b", "port": "a"}},
	      {"from": {"external": "b"}, "to": {"device": "b", "port": "b"}},
	      {"from": {"device": "b", "port": "c"}, "to": {"external": "c"}}
	    ]
	  }
	}`
	assertCode(t, doc, ErrDanglingConnection)
}

func TestRejectDuplicateDeviceID(t *testing.T) {
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [
	      {"id": "w", "kind": "waveguide", "params": {"length": 1, "neff": 2}},
	      {"id": "w", "kind": "waveguide", "params": {"length": 1, "neff": 2}}
	    ],
	    "connects": []
	  }
	}`
	assertCode(t, doc, ErrDuplicateID)
}

func TestRejectDirectedCycle(t *testing.T) {
	// 两个 2 端口器件首尾相连成有向环，并补上其余端口避免悬空报错先触发。
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [
	      {"id": "w1", "kind": "waveguide", "params": {"length": 1, "neff": 2}},
	      {"id": "w2", "kind": "waveguide", "params": {"length": 1, "neff": 2}}
	    ],
	    "connects": [
	      {"from": {"device": "w1", "port": "out"}, "to": {"device": "w2", "port": "in"}},
	      {"from": {"device": "w2", "port": "out"}, "to": {"device": "w1", "port": "in"}}
	    ]
	  }
	}`
	assertCode(t, doc, ErrTopologyCycle)
}

func TestRejectExternalInOutClash(t *testing.T) {
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [
	      {"id": "w1", "kind": "waveguide", "params": {"length": 1, "neff": 2}},
	      {"id": "w2", "kind": "waveguide", "params": {"length": 1, "neff": 2}}
	    ],
	    "connects": [
	      {"from": {"external": "p"}, "to": {"device": "w1", "port": "in"}},
	      {"from": {"device": "w1", "port": "out"}, "to": {"device": "w2", "port": "in"}},
	      {"from": {"device": "w2", "port": "out"}, "to": {"external": "p"}}
	    ]
	  }
	}`
	assertCode(t, doc, ErrPortDimensionMismatch)
}
