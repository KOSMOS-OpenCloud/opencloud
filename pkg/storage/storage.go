package storage

import (
	"errors"
)

var (
	// ErrStorageInitialization is returned when the storage initialization fails
	ErrStorageInitialization = errors.New("failed to initialize storage")

	// ErrStorageValidation is returned when the storage configuration is invalid
	ErrStorageValidation = errors.New("failed to validate storage configuration")
)
