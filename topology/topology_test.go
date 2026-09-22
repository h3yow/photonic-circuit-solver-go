package topology

import (
	"strings"
	"testing"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/model"
)

func compileJSON(t *testing.T, raw string) (*Compiled, engerrors.Kind) {
	t.Helper()
	m, err := model.Decode([]byte(raw))
	if err != nil {
		t.Fatalf("input:\n%s\ndecode failed: %v", raw, err)
	}
	c, err := Compile(m)
	return c, engerrors.KindOf(err)
}

const validWG = `{
  "version":"1.0",
  "devices":[{"id":"wg","type":"waveguide","params":{"phase":0.3}}],
  "connections":[],
  "external_ports":[{"device":"wg","port":"in"},{"device":"wg","port":"out"}]
}`

func TestCompile_Valid(t *testing.T) {
	c, kind := compileJSON(t, validWG)
	if kind != engerrors.KindNone {
		t.Fatalf("valid circuit rejected: %s", kind)
	}
	if len(c.Ports) != 2 || len(c.External) != 2 {
		t.Fatalf("expected 2 ports/2 external, got %d/%d", len(c.Ports), len(c.External))
	}
}

func TestCompile_TopologyErrorsAreDistinguishable(t *testing.T) {
	cases := []struct {
		name string
		json string
		want engerrors.Kind
	}{
		{
			"missing port on device",
			`{"version":"1.0","devices":[{"id":"wg","type":"waveguide","params":{}}],
			 "connections":[{"a":{"device":"wg","port":"nope"},"b":{"device":"wg","port":"out"}}],
			 "external_ports":[{"device":"wg","port":"in"}]}`,
			engerrors.KindMissingPort,
		},
		{
			"unknown device in connection",
			`{"version":"1.0","devices":[{"id":"wg","type":"waveguide","params":{}}],
			 "connections":[{"a":{"device":"ghost","port":"in"},"b":{"device":"wg","port":"out"}}],
			 "external_ports":[{"device":"wg","port":"in"}]}`,
			engerrors.KindMissingPort,
		},
		{
			"dangling unconnected port",
			`{"version":"1.0","devices":[{"id":"wg","type":"waveguide","params":{}}],
			 "connections":[],"external_ports":[{"device":"wg","port":"in"}]}`,
			engerrors.KindDanglingConnection,
		},
		{
			"doubly connected port",
			`{"version":"1.0",
			 "devices":[{"id":"a","type":"waveguide","params":{}},{"id":"b","type":"waveguide","params":{}},{"id":"c","type":"waveguide","params":{}}],
			 "connections":[
			   {"a":{"device":"a","port":"out"},"b":{"device":"b","port":"in"}},
			   {"a":{"device":"a","port":"out"},"b":{"device":"c","port":"in"}}],
			 "external_ports":[{"device":"a","port":"in"},{"device":"b","port":"out"},{"device":"c","port":"out"}]}`,
			engerrors.KindPortInUse,
		},
		{
			"input-to-input link",
			`{"version":"1.0",
			 "devices":[{"id":"a","type":"waveguide","params":{}},{"id":"b","type":"waveguide","params":{}}],
			 "connections":[{"a":{"device":"a","port":"in"},"b":{"device":"b","port":"in"}}],
			 "external_ports":[{"device":"a","port":"out"},{"device":"b","port":"out"}]}`,
			engerrors.KindInvalidParameter,
		},
		{
			"external port also connected",
			`{"version":"1.0",
			 "devices":[{"id":"a","type":"waveguide","params":{}},{"id":"b","type":"waveguide","params":{}}],
			 "connections":[{"a":{"device":"a","port":"out"},"b":{"device":"b","port":"in"}}],
			 "external_ports":[{"device":"a","port":"in"},{"device":"a","port":"out"},{"device":"b","port":"out"}]}`,
			engerrors.KindPortInUse,
		},
		{
			"multimode primitive dimension mismatch",
			`{"version":"1.0",
			 "devices":[{"id":"wg","type":"waveguide","modes":2,"params":{}}],
			 "connections":[],"external_ports":[{"device":"wg","port":"in"},{"device":"wg","port":"out"}]}`,
			engerrors.KindPortDimension,
		},
		{
			"directed cycle",
			`{"version":"1.0",
			 "devices":[{"id":"a","type":"beam_splitter","params":{}},{"id":"b","type":"beam_splitter","params":{}}],
			 "connections":[
			   {"a":{"device":"a","port":"out1"},"b":{"device":"b","port":"in1"}},
			   {"a":{"device":"b","port":"out1"},"b":{"device":"a","port":"in1"}}],
			 "external_ports":[{"device":"a","port":"in2"},{"device":"a","port":"out2"},
			                    {"device":"b","port":"in2"},{"device":"b","port":"out2"}]}`,
			engerrors.KindTopologyCycle,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, kind := compileJSON(t, tc.json)
			if kind != tc.want {
				t.Fatalf("input:\n%s\nexpected error kind=%q, got kind=%q", tc.json, tc.want, kind)
			}
		})
	}
}

func TestCompile_ErrorMessagesMentionPorts(t *testing.T) {
	raw := `{"version":"1.0","devices":[{"id":"wg","type":"waveguide","params":{}}],
	 "connections":[],"external_ports":[{"device":"wg","port":"in"}]}`
	_, kind := compileJSON(t, raw)
	if kind != engerrors.KindDanglingConnection {
		t.Fatalf("want dangling, got %q", kind)
	}
	m, _ := model.Decode([]byte(raw))
	_, err := Compile(m)
	if !strings.Contains(err.Error(), "wg.out") {
		t.Fatalf("error must identify the dangling port, got: %v", err)
	}
}
