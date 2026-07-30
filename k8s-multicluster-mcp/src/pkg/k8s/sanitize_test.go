package k8s

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestSanitizeResource_SecretRedaction(t *testing.T) {
	content := map[string]interface{}{
		"kind": "Secret",
		"metadata": map[string]interface{}{
			"name":      "my-secret",
			"namespace": "default",
		},
		"data": map[string]interface{}{
			"password": base64.StdEncoding.EncodeToString([]byte("supersecret")),
			"token":    base64.StdEncoding.EncodeToString([]byte("mytoken123")),
		},
		"stringData": map[string]interface{}{
			"config": "plain-text-value",
		},
	}

	result := SanitizeResource(content)

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		t.Fatal("result[\"data\"] is not map[string]interface{}")
	}
	for key, val := range data {
		s, ok := val.(string)
		if !ok {
			t.Fatalf("data[%s] is not a string", key)
		}
		if !strings.HasPrefix(s, "[REDACTED") {
			t.Errorf("Secret data[%s] was not redacted: %s", key, s)
		}
	}

	stringData, ok := result["stringData"].(map[string]interface{})
	if !ok {
		t.Fatal("result[\"stringData\"] is not map[string]interface{}")
	}
	for key, val := range stringData {
		s, ok := val.(string)
		if !ok {
			t.Fatalf("stringData[%s] is not a string", key)
		}
		if !strings.HasPrefix(s, "[REDACTED") {
			t.Errorf("Secret stringData[%s] was not redacted: %s", key, s)
		}
	}
}

func TestSanitizeResource_ConfigMapBinaryData(t *testing.T) {
	content := map[string]interface{}{
		"kind": "ConfigMap",
		"metadata": map[string]interface{}{
			"name": "my-cm",
		},
		"binaryData": map[string]interface{}{
			"cert.pem": base64.StdEncoding.EncodeToString(make([]byte, 4096)),
		},
		"data": map[string]interface{}{
			"config.yaml": "key: value",
		},
	}

	result := SanitizeResource(content)

	// binaryData should be redacted
	bd, ok := result["binaryData"].(map[string]interface{})
	if !ok {
		t.Fatal("result[\"binaryData\"] is not map[string]interface{}")
	}
	s, ok := bd["cert.pem"].(string)
	if !ok {
		t.Fatal("binaryData[\"cert.pem\"] is not a string")
	}
	if !strings.HasPrefix(s, "[REDACTED") {
		t.Errorf("ConfigMap binaryData was not redacted: %s", s)
	}

	// Regular data should be preserved
	d, ok := result["data"].(map[string]interface{})
	if !ok {
		t.Fatal("result[\"data\"] is not map[string]interface{}")
	}
	if d["config.yaml"] != "key: value" {
		t.Error("ConfigMap data was incorrectly modified")
	}
}

func TestSanitizeResource_LastAppliedConfigRemoved(t *testing.T) {
	content := map[string]interface{}{
		"kind": "Deployment",
		"metadata": map[string]interface{}{
			"name": "my-deploy",
			"annotations": map[string]interface{}{
				"kubectl.kubernetes.io/last-applied-configuration": `{"very":"large","json":"blob"}`,
				"custom-annotation": "keep-me",
			},
		},
	}

	result := SanitizeResource(content)

	meta, ok := result["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("result[\"metadata\"] is not map[string]interface{}")
	}
	annotations, ok := meta["annotations"].(map[string]interface{})
	if !ok {
		t.Fatal("meta[\"annotations\"] is not map[string]interface{}")
	}

	if _, exists := annotations["kubectl.kubernetes.io/last-applied-configuration"]; exists {
		t.Error("last-applied-configuration annotation was not removed")
	}
	if annotations["custom-annotation"] != "keep-me" {
		t.Error("custom annotation was incorrectly removed")
	}
}

func TestSanitizeResource_ManagedFieldsRemoved(t *testing.T) {
	content := map[string]interface{}{
		"kind": "Pod",
		"metadata": map[string]interface{}{
			"name":          "my-pod",
			"managedFields": []interface{}{"field1", "field2"},
			"selfLink":      "/api/v1/pods/my-pod",
		},
	}

	result := SanitizeResource(content)
	meta, ok := result["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("result[\"metadata\"] is not map[string]interface{}")
	}

	if _, exists := meta["managedFields"]; exists {
		t.Error("managedFields was not removed")
	}
	if _, exists := meta["selfLink"]; exists {
		t.Error("selfLink was not removed")
	}
}

func TestSanitizeResource_DeepBase64Detection(t *testing.T) {
	// Create a large base64 string that doesn't live in Secret.data
	rawCert := make([]byte, 2048)
	for i := range rawCert {
		rawCert[i] = byte(i % 256)
	}
	b64Cert := base64.StdEncoding.EncodeToString(rawCert)

	content := map[string]interface{}{
		"kind": "CustomResource",
		"metadata": map[string]interface{}{
			"name": "my-cr",
		},
		"spec": map[string]interface{}{
			"tlsCert": b64Cert,
			"name":    "normal-string",
			"nested": map[string]interface{}{
				"caBundle": b64Cert,
			},
		},
	}

	result := SanitizeResource(content)

	spec, ok := result["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("result[\"spec\"] is not map[string]interface{}")
	}
	tlsCert, ok := spec["tlsCert"].(string)
	if !ok {
		t.Fatal("spec[\"tlsCert\"] is not a string")
	}
	if !strings.Contains(tlsCert, "BASE64_DATA") {
		t.Error("large base64 in spec.tlsCert was not detected")
	}
	if spec["name"] != "normal-string" {
		t.Error("normal string was incorrectly modified")
	}

	nested, ok := spec["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("spec[\"nested\"] is not map[string]interface{}")
	}
	caBundle, ok := nested["caBundle"].(string)
	if !ok {
		t.Fatal("nested[\"caBundle\"] is not a string")
	}
	if !strings.Contains(caBundle, "BASE64_DATA") {
		t.Error("large base64 in spec.nested.caBundle was not detected")
	}
}

func TestSanitizeResource_PreservesShortStrings(t *testing.T) {
	content := map[string]interface{}{
		"kind": "Service",
		"metadata": map[string]interface{}{
			"name": "my-svc",
		},
		"spec": map[string]interface{}{
			"clusterIP": "10.0.0.1",
			"type":      "ClusterIP",
		},
	}

	result := SanitizeResource(content)
	spec, ok := result["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("result[\"spec\"] is not map[string]interface{}")
	}

	if spec["clusterIP"] != "10.0.0.1" {
		t.Error("short string clusterIP was incorrectly modified")
	}
	if spec["type"] != "ClusterIP" {
		t.Error("short string type was incorrectly modified")
	}
}

func TestSanitizeText_InlineBase64(t *testing.T) {
	blob := base64.StdEncoding.EncodeToString(make([]byte, 512))
	input := "apiVersion: v1\nkind: Secret\ndata:\n  password: " + blob + "\n  name: test\n"

	result := SanitizeText(input)

	if strings.Contains(result, blob) {
		t.Error("inline base64 blob was not replaced")
	}
	if !strings.Contains(result, "[REDACTED") && !strings.Contains(result, "BASE64_DATA") {
		t.Error("placeholder was not inserted")
	}
	if strings.Contains(result, "name: test") {
		t.Error("Secret data values should be redacted even when not base64")
	}
}

func TestSanitizeText_ShortSensitiveAssignments(t *testing.T) {
	input := "password=short-secret token: abc123 authorization: Bearer xyz"
	result := SanitizeText(input)

	for _, leaked := range []string{"short-secret", "abc123", "Bearer xyz"} {
		if strings.Contains(result, leaked) {
			t.Fatalf("sensitive value %q leaked in %q", leaked, result)
		}
	}
	if strings.Count(result, "[REDACTED]") < 3 {
		t.Fatalf("expected redaction markers, got %q", result)
	}
}

func TestSanitizeText_SecretYAMLBlock(t *testing.T) {
	input := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: test\ndata:\n  username: YWRtaW4=\n  password: c2VjcmV0\n---\nkind: ConfigMap\ndata:\n  keep: visible\n"
	result := SanitizeText(input)

	if strings.Contains(result, "YWRtaW4=") || strings.Contains(result, "c2VjcmV0") {
		t.Fatalf("secret YAML data leaked: %s", result)
	}
	if !strings.Contains(result, "keep: visible") {
		t.Fatalf("non-secret YAML data was unexpectedly redacted: %s", result)
	}
}

func TestSanitizeForOutput_KeyAwareRedaction(t *testing.T) {
	input := map[string]interface{}{
		"username": "admin",
		"password": "short-secret",
		"nested": map[string]interface{}{
			"apiKey": "abc123",
		},
	}

	result := SanitizeForOutput(input).(map[string]interface{})
	if result["username"] != "admin" {
		t.Fatalf("non-sensitive username changed: %#v", result["username"])
	}
	if result["password"] == "short-secret" {
		t.Fatal("password was not redacted")
	}
	nested := result["nested"].(map[string]interface{})
	if nested["apiKey"] == "abc123" {
		t.Fatal("nested apiKey was not redacted")
	}
}

func TestSanitizeText_Truncation(t *testing.T) {
	// Create a string larger than maxResponseBytes
	large := strings.Repeat("a", maxResponseBytes+1000)
	result := SanitizeText(large)

	if len(result) > maxResponseBytes+200 { // allow room for the truncation message
		t.Errorf("output was not truncated: got %d bytes", len(result))
	}
	if !strings.Contains(result, "TRUNCATED") {
		t.Error("truncation message was not added")
	}
}

func TestTruncateJSON(t *testing.T) {
	small := []byte(`{"key":"value"}`)
	if !bytes.Equal(TruncateJSON(small), small) {
		t.Error("small JSON was incorrectly truncated")
	}

	large := make([]byte, maxResponseBytes+1)
	for i := range large {
		large[i] = 'x'
	}
	result := TruncateJSON(large)
	if len(result) >= len(large) {
		t.Error("large JSON was not truncated")
	}
	if !strings.Contains(string(result), "response too large") {
		t.Error("truncation error message not present")
	}
}

func TestLooksLikeText(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{"JSON", `{"key": "value", "number": 42}`, true},
		{"YAML", "apiVersion: v1\nkind: Pod\nmetadata:\n  name: test", true},
		{"prose", "This is a normal sentence with spaces and punctuation.", true},
		{"base64", "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo=", false},
		{"long base64", strings.Repeat("ABCD", 100), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeText(tt.input)
			if got != tt.expect {
				preview := tt.input
				if len(preview) > 40 {
					preview = preview[:40]
				}
				t.Errorf("looksLikeText(%q) = %v, want %v", preview, got, tt.expect)
			}
		})
	}
}
