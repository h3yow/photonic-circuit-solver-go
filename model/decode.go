package model

import (
	"bytes"
	"encoding/json"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
)

// Decode parses a versioned JSON model with strict field checking.
//
// Default semantics are explicit and stable across supported versions:
// unknown fields are never silently ignored (KindUnknownField), and versions
// outside [MinVersion, MaxVersion] are rejected (KindUnsupportedVersion).
func Decode(raw []byte) (*CircuitModel, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var m CircuitModel
	if err := dec.Decode(&m); err != nil {
		return nil, engerrors.Wrap(engerrors.KindModelDecode, err, "failed to decode circuit model")
	}
	// Reject trailing non-whitespace tokens.
	if dec.More() {
		return nil, engerrors.New(engerrors.KindModelDecode, "unexpected trailing data after model")
	}
	if err := CheckVersion(m.Version); err != nil {
		return nil, err
	}
	return &m, nil
}

// CheckVersion verifies that v lies in the supported version window.
func CheckVersion(v string) error {
	if v != Version1_0 && v != Version1_1 {
		return engerrors.New(engerrors.KindUnsupportedVersion,
			"model version %q not supported; supported: %s..%s", v, MinVersion, MaxVersion)
	}
	return nil
}

// Encode serializes a model with the given version. The passed model is never
// mutated (safe for concurrent calls on a shared cached model).
func Encode(m *CircuitModel, version string) ([]byte, error) {
	if err := CheckVersion(version); err != nil {
		return nil, err
	}
	cp := *m
	cp.Version = version
	raw, err := json.MarshalIndent(&cp, "", "  ")
	if err != nil {
		return nil, engerrors.Wrap(engerrors.KindModelDecode, err, "failed to encode circuit model")
	}
	return raw, nil
}
