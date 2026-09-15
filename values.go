package zconfig

import (
	"encoding/json"
	"fmt"
)

// Values is an immutable snapshot returned by a batch query.
type Values struct {
	values map[string]any
}

func newValues(values map[string]any) *Values {
	return &Values{values: values}
}

func (v *Values) GetAny(key string) (any, bool) {
	if v == nil {
		return nil, false
	}
	value, ok := v.values[key]
	return value, ok
}

func (v *Values) GetString(key string) (string, error) {
	value, ok := v.GetAny(key)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrKeyNotRegistered, key)
	}
	result, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%w: %s is not string", ErrValueTypeMismatch, key)
	}
	return result, nil
}

func (v *Values) GetInt64(key string) (int64, error) {
	value, ok := v.GetAny(key)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrKeyNotRegistered, key)
	}
	result, ok := toInt64(value)
	if !ok {
		return 0, fmt.Errorf("%w: %s is not number", ErrValueTypeMismatch, key)
	}
	return result, nil
}

func (v *Values) GetBool(key string) (bool, error) {
	value, ok := v.GetAny(key)
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrKeyNotRegistered, key)
	}
	result, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%w: %s is not boolean", ErrValueTypeMismatch, key)
	}
	return result, nil
}

// Map returns a copy so callers cannot modify the snapshot internally.
func (v *Values) Map() map[string]any {
	if v == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(v.values))
	for key, value := range v.values {
		result[key] = value
	}
	return result
}

func (v *Values) Len() int {
	if v == nil {
		return 0
	}
	return len(v.values)
}

// Unmarshal copies this snapshot into target using JSON field names and json
// tags. Target must be a non-nil pointer to a struct, map, or other value
// accepted by json.Unmarshal.
func (v *Values) Unmarshal(target any) error {
	data, err := json.Marshal(v.Map())
	if err != nil {
		return fmt.Errorf("marshal config values: %w", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("unmarshal config values: %w", err)
	}
	return nil
}
