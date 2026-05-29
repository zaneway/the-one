package adapter

import "errors"

var (
	errInvalidAtomicShape = errors.New("INVALID_ATOMIC_SHAPE")
	errMissingEventType   = errors.New("missing event_type")
	errWrongTransport     = errors.New("WRONG_TRANSPORT")
	errInvalidSession     = errors.New("INVALID_SESSION")
	errSessionNotReady    = errors.New("SESSION_NOT_READY")
)
