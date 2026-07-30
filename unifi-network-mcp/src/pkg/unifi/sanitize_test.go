package unifi

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSanitizeForOutputRedactsSensitiveKeys(t *testing.T) {
	payload := map[string]interface{}{
		"apiKey": "secret-api-key",
		"nested": map[string]interface{}{
			"authorization": "Bearer token",
			"name":          "keep",
		},
	}

	out := SanitizeForOutput(payload).(map[string]interface{})
	if out["apiKey"] == "secret-api-key" {
		t.Fatal("apiKey was not redacted")
	}
	nested := out["nested"].(map[string]interface{})
	if nested["authorization"] == "Bearer token" {
		t.Fatal("authorization was not redacted")
	}
	if nested["name"] != "keep" {
		t.Fatal("non-sensitive value was modified")
	}
}

func TestSanitizeForOutputReplacesLargeBase64(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(make([]byte, 512))
	out := SanitizeForOutput(map[string]interface{}{"blob": encoded}).(map[string]interface{})
	if !strings.Contains(out["blob"].(string), "BASE64_DATA") {
		t.Fatalf("large base64 was not replaced: %v", out["blob"])
	}
}

func TestTruncateJSON(t *testing.T) {
	small := []byte(`{"ok":true}`)
	if string(TruncateJSON(small)) != string(small) {
		t.Fatal("small JSON was truncated")
	}

	large := make([]byte, maxResponseBytes+1)
	for i := range large {
		large[i] = 'x'
	}
	out := string(TruncateJSON(large))
	if !strings.Contains(out, "response too large") {
		t.Fatalf("expected truncation error, got %s", out)
	}
}
