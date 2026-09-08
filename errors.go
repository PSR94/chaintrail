package chaintrail

import (
	"errors"
	"fmt"
)

var (
	ErrAlreadyInitialized = errors.New("journal already initialized")
	ErrNotInitialized     = errors.New("journal is not initialized")
)

type IntegrityError struct {
	Message string
}

func (e *IntegrityError) Error() string { return e.Message }

func integrityf(format string, args ...any) error {
	return &IntegrityError{Message: fmt.Sprintf(format, args...)}
}

func IsIntegrityError(err error) bool {
	var target *IntegrityError
	return errors.As(err, &target)
}
