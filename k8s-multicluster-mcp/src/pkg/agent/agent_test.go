package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func newTestAgentClient() *Client {
	return &Client{
		config: Config{
			BaseURL:        "http://llm.local:8080/v1",
			APIKey:         "test-key",
			Model:          "private-llm/default-model",
			BinaryPath:     "/usr/local/bin/k8s-mcp-server",
			KubeconfigPath: "/home/appuser/.kube/config",
		},
	}
}

func readConfig(t *testing.T, path string) map[string]interface{} {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}
	return config
}

func TestWriteConfigUsesExplicitChildMCPServer(t *testing.T) {
	client := newTestAgentClient()
	configPath := filepath.Join(t.TempDir(), "opencode.json")

	if err := client.writeConfig(configPath, RunParams{}); err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	config := readConfig(t, configPath)
	if config["$schema"] != "https://opencode.ai/config.json" {
		t.Fatalf("unexpected schema: %v", config["$schema"])
	}
	if config["model"] != "private-llm/default-model" {
		t.Fatalf("unexpected model: %v", config["model"])
	}
	if config["share"] != "disabled" {
		t.Fatalf("share should be disabled, got %v", config["share"])
	}
	if config["autoupdate"] != false {
		t.Fatalf("autoupdate should be false, got %v", config["autoupdate"])
	}

	mcp := config["mcp"].(map[string]interface{})
	k8s := mcp["k8s"].(map[string]interface{})
	if k8s["type"] != "local" {
		t.Fatalf("unexpected mcp type: %v", k8s["type"])
	}
	if k8s["enabled"] != true {
		t.Fatalf("mcp should be enabled, got %v", k8s["enabled"])
	}
	if k8s["timeout"] != float64(30000) {
		t.Fatalf("unexpected mcp timeout: %v", k8s["timeout"])
	}

	gotCommand := k8s["command"].([]interface{})
	wantCommand := []interface{}{
		"/usr/local/bin/k8s-mcp-server",
		"--mode",
		"stdio",
		"--no-agent",
	}
	if !reflect.DeepEqual(gotCommand, wantCommand) {
		t.Fatalf("command = %#v, want %#v", gotCommand, wantCommand)
	}

	env := k8s["environment"].(map[string]interface{})
	if env["KUBECONFIG"] != "/home/appuser/.kube/config" {
		t.Fatalf("unexpected KUBECONFIG: %v", env["KUBECONFIG"])
	}
}

func TestWriteConfigReadOnlyAndModelOverride(t *testing.T) {
	client := newTestAgentClient()
	configPath := filepath.Join(t.TempDir(), "opencode.json")

	if err := client.writeConfig(configPath, RunParams{ReadOnly: true, Model: "override/provider-model"}); err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	config := readConfig(t, configPath)
	if config["model"] != "override/provider-model" {
		t.Fatalf("unexpected model: %v", config["model"])
	}

	provider := config["provider"].(map[string]interface{})
	if _, ok := provider["override"]; !ok {
		t.Fatalf("override provider was not configured: %#v", provider)
	}

	mcp := config["mcp"].(map[string]interface{})
	k8s := mcp["k8s"].(map[string]interface{})
	gotCommand := k8s["command"].([]interface{})
	wantCommand := []interface{}{
		"/usr/local/bin/k8s-mcp-server",
		"--mode",
		"stdio",
		"--no-agent",
		"--read-only",
	}
	if !reflect.DeepEqual(gotCommand, wantCommand) {
		t.Fatalf("command = %#v, want %#v", gotCommand, wantCommand)
	}
}

func TestWriteConfigCopiesCapabilityEnv(t *testing.T) {
	t.Setenv("MCP_ENABLE_EXEC", "true")
	t.Setenv("MCP_ENABLE_KUBECTL", "true")

	client := newTestAgentClient()
	configPath := filepath.Join(t.TempDir(), "opencode.json")

	if err := client.writeConfig(configPath, RunParams{}); err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	config := readConfig(t, configPath)
	mcp := config["mcp"].(map[string]interface{})
	k8s := mcp["k8s"].(map[string]interface{})
	env := k8s["environment"].(map[string]interface{})
	if env["MCP_ENABLE_EXEC"] != "true" {
		t.Fatalf("MCP_ENABLE_EXEC was not propagated: %#v", env)
	}
	if env["MCP_ENABLE_KUBECTL"] != "true" {
		t.Fatalf("MCP_ENABLE_KUBECTL was not propagated: %#v", env)
	}
}

func TestParseNDJSONTextEvents(t *testing.T) {
	client := newTestAgentClient()
	data := []byte(`{"type":"step_start","part":{"type":"step-start"}}` + "\n" +
		`{"type":"text","part":{"type":"text","text":"hello "}}` + "\n" +
		`{"type":"text","part":{"type":"text","text":"world"}}` + "\n")

	got := client.parseNDJSON(data)
	if got != "hello world" {
		t.Fatalf("parseNDJSON() = %q, want %q", got, "hello world")
	}
}

func TestParseNDJSONErrorEvent(t *testing.T) {
	client := newTestAgentClient()
	data := []byte(`{"type":"error","error":{"name":"APIError","data":{"message":"rate limit exceeded"}}}` + "\n")

	got := client.parseNDJSON(data)
	if got != "rate limit exceeded" {
		t.Fatalf("parseNDJSON() = %q, want %q", got, "rate limit exceeded")
	}
}
