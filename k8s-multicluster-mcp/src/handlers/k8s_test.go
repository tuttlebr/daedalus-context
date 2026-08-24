package handlers

import (
	"strings"
	"testing"
	"time"
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

func TestGetBoundedIntArg(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		want    int
		wantErr string
	}{
		{name: "default", args: map[string]interface{}{}, want: 60},
		{name: "integer", args: map[string]interface{}{"window": 120}, want: 120},
		{name: "JSON number", args: map[string]interface{}{"window": float64(300)}, want: 300},
		{name: "fraction", args: map[string]interface{}{"window": 1.5}, wantErr: "whole number"},
		{name: "wrong type", args: map[string]interface{}{"window": "300"}, wantErr: "must be a number"},
		{name: "too small", args: map[string]interface{}{"window": 0}, wantErr: "between 1 and 600"},
		{name: "too large", args: map[string]interface{}{"window": 601}, wantErr: "between 1 and 600"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getBoundedIntArg(tt.args, "window", 60, 1, 600)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFormatPodLogsMakesTimeWindowExplicit(t *testing.T) {
	observedAt := time.Date(2026, time.August, 18, 16, 30, 0, 0, time.UTC)
	got := formatPodLogs("2026-08-18T16:29:00Z ready\n", observedAt, 3600, 100)

	for _, want := range []string{
		"2026-08-18T15:30:00Z through 2026-08-18T16:30:00Z",
		"last 3600 seconds",
		"at most 100 lines per container",
		"2026-08-18T16:29:00Z ready",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted logs missing %q: %s", want, got)
		}
	}
}

func TestFormatPodLogsExplainsEmptyWindow(t *testing.T) {
	got := formatPodLogs("\n", time.Date(2026, time.August, 18, 16, 30, 0, 0, time.UTC), 300, 50)
	if !strings.Contains(got, "No log entries were returned for this time window") {
		t.Fatalf("empty response did not explain the queried window: %s", got)
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
