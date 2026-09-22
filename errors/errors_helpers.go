package errors

import "errors"

// As is a thin re-export of stdlib errors.As for internal use.
func As(err error, target any) bool {
	return errors.As(err, target)
}

// Is re-exports stdlib errors.Is.
func Is(err, target error) bool {
	return errors.Is(err, target)
}
