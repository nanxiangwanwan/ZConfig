package zconfig

import (
	"errors"
	"testing"
)

func TestValidateAttributeNormalizesNumber(t *testing.T) {
	attribute, err := validateAttribute(ConfigAttribute{
		Key:          "limit",
		VType:        ValueTypeNumber,
		DefaultValue: float64(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := attribute.DefaultValue.(int64); !ok || value != 10 {
		t.Fatalf("got %#v, want int64(10)", attribute.DefaultValue)
	}
}

func TestValidateAttributeRejectsFraction(t *testing.T) {
	_, err := validateAttribute(ConfigAttribute{
		Key:          "limit",
		VType:        ValueTypeNumber,
		DefaultValue: 10.5,
	})
	if !errors.Is(err, ErrValueTypeMismatch) {
		t.Fatalf("got %v, want ErrValueTypeMismatch", err)
	}
}

func TestRequiredString(t *testing.T) {
	_, err := validateValue(ConfigAttribute{
		Key:        "name",
		VType:      ValueTypeString,
		IsRequired: true,
	}, "  ")
	if !errors.Is(err, ErrRequired) {
		t.Fatalf("got %v, want ErrRequired", err)
	}
}

func TestRegexp(t *testing.T) {
	_, err := validateValue(ConfigAttribute{
		Key:    "code",
		VType:  ValueTypeString,
		RegExp: `^[A-Z]{3}$`,
	}, "abc")
	if !errors.Is(err, ErrRegExpMismatch) {
		t.Fatalf("got %v, want ErrRegExpMismatch", err)
	}
}
