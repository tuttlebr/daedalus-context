package handlers

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/tuttlebr/kubernetes-mcp-server/pkg/k8s"
)

// marshalSafe serializes, redacts, and size-bounds arbitrary tool output.
func marshalSafe(v interface{}) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var decoded interface{}
	if err := json.Unmarshal(data, &decoded); err == nil {
		data, err = json.Marshal(k8s.SanitizeForOutput(decoded))
		if err != nil {
			return nil, err
		}
	}

	return k8s.TruncateJSON(data), nil
}

func apiResourceResponse(resources []map[string]interface{}, query string, limit int) map[string]interface{} {
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]map[string]interface{}, 0, len(resources))
	for _, resource := range resources {
		if query == "" || apiResourceMatches(resource, query) {
			filtered = append(filtered, resource)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		left := strings.ToLower(stringField(filtered[i], "group") + "/" + stringField(filtered[i], "kind") + "/" + stringField(filtered[i], "name"))
		right := strings.ToLower(stringField(filtered[j], "group") + "/" + stringField(filtered[j], "kind") + "/" + stringField(filtered[j], "name"))
		return left < right
	})

	return boundedMapItems(filtered, limit,
		"Refine query or scope flags to retrieve omitted API resource types")
}

func apiResourceMatches(resource map[string]interface{}, query string) bool {
	for _, field := range []string{"name", "singularName", "kind", "group", "version"} {
		if strings.Contains(strings.ToLower(stringField(resource, field)), query) {
			return true
		}
	}
	return false
}

func resourceListResponse(resources []map[string]interface{}, includeDetails bool, limit int) map[string]interface{} {
	items := resources
	if !includeDetails {
		items = make([]map[string]interface{}, 0, len(resources))
		for _, resource := range resources {
			items = append(items, summarizeResource(resource))
		}
	}
	return boundedMapItems(items, limit,
		"Use namespace, labelSelector, fieldSelector, or a smaller limit; use getResource for full details")
}

func summarizeResource(resource map[string]interface{}) map[string]interface{} {
	summary := map[string]interface{}{}
	copyNonEmptyString(summary, resource, "apiVersion")
	copyNonEmptyString(summary, resource, "kind")

	if metadata, ok := resource["metadata"].(map[string]interface{}); ok {
		copyNonEmptyStringAs(summary, metadata, "name", "name")
		copyNonEmptyStringAs(summary, metadata, "namespace", "namespace")
		copyNonEmptyStringAs(summary, metadata, "creationTimestamp", "created")
		copyNonEmptyStringAs(summary, metadata, "deletionTimestamp", "deletingSince")
	}

	copySelectedScalars(summary, "spec", resource["spec"], []string{
		"replicas", "nodeName", "type", "clusterIP", "storageClassName", "volumeName",
	})
	copySelectedScalars(summary, "status", resource["status"], []string{
		"phase", "reason", "replicas", "readyReplicas", "availableReplicas", "updatedReplicas",
		"unavailableReplicas", "currentReplicas", "active", "succeeded", "failed",
	})

	return summary
}

func copySelectedScalars(destination map[string]interface{}, destinationKey string, value interface{}, keys []string) {
	source, ok := value.(map[string]interface{})
	if !ok {
		return
	}
	selected := map[string]interface{}{}
	for _, key := range keys {
		value, exists := source[key]
		if !exists {
			continue
		}
		switch value.(type) {
		case string, float64, bool, int, int32, int64:
			selected[key] = value
		}
	}
	if len(selected) > 0 {
		destination[destinationKey] = selected
	}
}

func eventListResponse(events []map[string]interface{}, limit int) map[string]interface{} {
	ordered := append([]map[string]interface{}(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return eventTimestamp(ordered[i]).After(eventTimestamp(ordered[j]))
	})
	return boundedMapItems(ordered, limit,
		"Use namespace or labelSelector to retrieve a narrower event set")
}

func eventTimestamp(event map[string]interface{}) time.Time {
	for _, key := range []string{"lastTime", "firstTime"} {
		switch value := event[key].(type) {
		case time.Time:
			return value
		case string:
			if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}

func boundedMapItems(items []map[string]interface{}, limit int, hint string) map[string]interface{} {
	returned := len(items)
	if returned > limit {
		returned = limit
	}
	response := map[string]interface{}{
		"items":    items[:returned],
		"returned": returned,
		"matched":  len(items),
	}
	if returned < len(items) {
		response["truncated"] = true
		response["hint"] = hint
	}
	return response
}

func stringField(source map[string]interface{}, key string) string {
	value, _ := source[key].(string)
	return value
}

func copyNonEmptyString(destination, source map[string]interface{}, key string) {
	copyNonEmptyStringAs(destination, source, key, key)
}

func copyNonEmptyStringAs(destination, source map[string]interface{}, sourceKey, destinationKey string) {
	if value := stringField(source, sourceKey); value != "" {
		destination[destinationKey] = value
	}
}
