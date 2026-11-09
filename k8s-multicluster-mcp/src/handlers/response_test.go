package handlers

import (
	"testing"
	"time"
)

func TestAPIResourceResponseFiltersSortsAndLimits(t *testing.T) {
	resources := []map[string]interface{}{
		{"name": "deployments", "kind": "Deployment", "group": "apps"},
		{"name": "pods", "kind": "Pod", "group": ""},
		{"name": "podmonitors", "kind": "PodMonitor", "group": "monitoring.coreos.com"},
	}

	response := apiResourceResponse(resources, "pod", 1)
	if response["matched"] != 2 || response["returned"] != 1 || response["truncated"] != true {
		t.Fatalf("unexpected response metadata: %#v", response)
	}
	items, ok := response["items"].([]map[string]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected one response item, got %#v", response["items"])
	}
	if items[0]["name"] != "pods" {
		t.Fatalf("unexpected first filtered resource: %#v", items[0])
	}
}

func TestResourceListResponseUsesCompactSummariesByDefault(t *testing.T) {
	resources := []map[string]interface{}{
		{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":              "api",
				"namespace":         "daedalus",
				"creationTimestamp": "2026-08-26T12:00:00Z",
				"labels":            map[string]interface{}{"large": "omitted"},
			},
			"spec": map[string]interface{}{
				"replicas": float64(3),
				"template": map[string]interface{}{"very": "verbose"},
			},
			"status": map[string]interface{}{
				"readyReplicas": float64(2),
				"conditions":    []interface{}{map[string]interface{}{"message": "verbose"}},
			},
		},
	}

	response := resourceListResponse(resources, false, 50)
	items, ok := response["items"].([]map[string]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected one response item, got %#v", response["items"])
	}
	item := items[0]
	if item["name"] != "api" || item["namespace"] != "daedalus" {
		t.Fatalf("identity fields missing from summary: %#v", item)
	}
	if _, exists := item["metadata"]; exists {
		t.Fatalf("full metadata leaked into compact summary: %#v", item)
	}
	status, ok := item["status"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a status object, got %#v", item["status"])
	}
	if status["readyReplicas"] != float64(2) {
		t.Fatalf("common status fields missing from summary: %#v", status)
	}
	if _, exists := status["conditions"]; exists {
		t.Fatalf("verbose conditions leaked into summary: %#v", status)
	}
}

func TestEventListResponseReturnsNewestFirst(t *testing.T) {
	events := []map[string]interface{}{
		{"name": "old", "lastTime": time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)},
		{"name": "new", "lastTime": time.Date(2026, 8, 26, 11, 0, 0, 0, time.UTC)},
	}

	response := eventListResponse(events, 1)
	items, ok := response["items"].([]map[string]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected one response item, got %#v", response["items"])
	}
	if items[0]["name"] != "new" {
		t.Fatalf("newest event was not returned first: %#v", items)
	}
	if response["matched"] != 2 || response["truncated"] != true {
		t.Fatalf("unexpected response metadata: %#v", response)
	}
}
