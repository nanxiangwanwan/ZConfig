package zconfig

import "errors"

var (
	ErrClosed            = errors.New("zconfig is closed")
	ErrEmptyKey          = errors.New("config key is empty")
	ErrKeyNotRegistered  = errors.New("config key is not registered")
	ErrDuplicateKey      = errors.New("config key is already registered")
	ErrInvalidValueType  = errors.New("invalid config value type")
	ErrValueTypeMismatch = errors.New("config value type mismatch")
	ErrRequired          = errors.New("required config value cannot be empty")
	ErrRegExpMismatch    = errors.New("config value does not match regexp")
	ErrReadOnly          = errors.New("config is read-only")
)
