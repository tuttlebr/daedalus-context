package handlers

import (
	"strings"
	"testing"
)

func TestGetStringArg(t *testing.T) {
	args := map[string]interface{}{
		"present": "hello",
		"number":  42,
		"empty":   "",
	}

	tests := []struct {
		key      string
		fallback string
		want     string
	}{
		{"present", "default", "hello"},
		{"missing", "default", "default"},
		{"number", "default", "default"},
		{"empty", "default", ""},
	}

	for _, tt := range tests {
		got := getStringArg(args, tt.key, tt.fallback)
		if got != tt.want {
			t.Errorf("getStringArg(args, %q, %q) = %q, want %q", tt.key, tt.fallback, got, tt.want)
		}
	}
}

func TestGetBoolArg(t *testing.T) {
	args := map[string]interface{}{
		"truthy": true,
		"falsy":  false,
		"string": "true",
		"number": 1,
	}

	tests := []struct {
		key      string
		fallback bool
		want     bool
	}{
		{"truthy", false, true},
		{"falsy", true, false},
		{"missing", true, true},
		{"missing", false, false},
		{"string", false, false},
		{"number", false, false},
	}

	for _, tt := range tests {
		got := getBoolArg(args, tt.key, tt.fallback)
		if got != tt.want {
			t.Errorf("getBoolArg(args, %q, %v) = %v, want %v", tt.key, tt.fallback, got, tt.want)
		}
	}
}

func TestGetRequiredStringArg(t *testing.T) {
	args := map[string]interface{}{
		"present": "value",
		"empty":   "",
		"number":  123,
	}

	t.Run("present value", func(t *testing.T) {
		val, err := getRequiredStringArg(args, "present")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "value" {
			t.Errorf("got %q, want %q", val, "value")
		}
	})

	t.Run("missing key", func(t *testing.T) {
		_, err := getRequiredStringArg(args, "missing")
		if err == nil {
			t.Fatal("expected error for missing key")
		}
	})

	t.Run("empty string", func(t *testing.T) {
		_, err := getRequiredStringArg(args, "empty")
		if err == nil {
			t.Fatal("expected error for empty string")
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		_, err := getRequiredStringArg(args, "number")
		if err == nil {
			t.Fatal("expected error for wrong type")
		}
	})
}

func TestMarshalSafeRedactsSensitiveStructuredOutput(t *testing.T) {
	payload := map[string]interface{}{
		"manifest": "apiVersion: v1\nkind: Secret\ndata:\n  password: c2VjcmV0\n",
		"config": map[string]interface{}{
			"password": "short-secret",
		},
	}

	data, err := marshalSafe(payload)
	if err != nil {
		t.Fatalf("marshalSafe failed: %v", err)
	}
	output := string(data)
	for _, leaked := range []string{"c2VjcmV0", "short-secret"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("sensitive value %q leaked in %s", leaked, output)
		}
	}
}
