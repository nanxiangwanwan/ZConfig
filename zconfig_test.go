package zconfig

import (
	"errors"
	"regexp"
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

func TestRegisterJSONReadOnly(t *testing.T) {
	z := newZConfig(nil, nil, false)
	if err := z.RegisterJSON([]byte(`{"key":"server_version","vType":"string","readOnly":true}`)); err != nil {
		t.Fatal(err)
	}
	attributes := z.GetConfigAttributes()
	if len(attributes) != 1 || !attributes[0].ReadOnly {
		t.Fatalf("got %#v, want one read-only attribute", attributes)
	}
}

func TestCommonRegExps(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		match   string
		miss    string
	}{
		{"email", RegExpEmail, "name@example.com", "name@example"},
		{"non-negative int", RegExpNonNegativeInt, "0", "01"},
		{"positive int", RegExpPositiveInt, "12", "0"},
		{"float", RegExpFloat, "-1.5", "1e3"},
		{"https url", RegExpHTTPSURL, "https://example.com/a?q=1", "http://example.com"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			re := regexp.MustCompile(test.pattern)
			if !re.MatchString(test.match) {
				t.Fatalf("%q should match", test.match)
			}
			if re.MatchString(test.miss) {
				t.Fatalf("%q should not match", test.miss)
			}
		})
	}
}
