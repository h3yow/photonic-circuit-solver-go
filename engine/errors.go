package engine

import (
	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/model"
)

func errBatchTooLarge(n, limit int) error {
	return engerrors.New(engerrors.KindLimitExceeded,
		"batch size %d exceeds limit %d (policy=reject)", n, limit)
}

func (e *Engine) missingShifter(id string) error {
	return engerrors.New(engerrors.KindMissingPort, "phase shifter %q not found", id)
}

func (e *Engine) notAShifter(id string, t model.DeviceType) error {
	return engerrors.New(engerrors.KindInvalidParameter, "device %q is %s, not a phase shifter", id, t)
}
