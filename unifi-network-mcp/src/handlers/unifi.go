package handlers

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tuttlebr/unifi-network-mcp/pkg/unifi"
)

// ClientDefaults contains optional server-side connection defaults. Empty
// fields preserve strict per-call requirements.
type ClientDefaults struct {
	BaseURL            string
	APIKey             string
	InsecureSkipVerify *bool
	Locked             bool
}

// EndpointHandler creates an MCP handler for a registered UniFi API endpoint.
func EndpointHandler(endpoint unifi.Endpoint, defaults ClientDefaults) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return mcp.NewToolResultError("invalid arguments: expected a JSON object"), nil
		}

		client, err := clientFromArgs(args, defaults)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		path, err := buildPath(endpoint, args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		query, err := buildQuery(endpoint, args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body, err := buildRequestBody(endpoint, args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		data, err := client.DoJSON(ctx, endpoint.Method, path, query, body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}

func clientFromArgs(args map[string]interface{}, defaults ClientDefaults) (*unifi.Client, error) {
	if defaults.Locked {
		for _, key := range []string{"baseUrl", "apiKey", "insecureSkipVerify"} {
			if _, supplied := args[key]; supplied {
				return nil, fmt.Errorf("%s cannot be overridden in fixed-controller mode", key)
			}
		}
	}
	baseURL := strings.TrimSpace(getStringArg(args, "baseUrl", defaults.BaseURL))
	if baseURL == "" {
		return nil, fmt.Errorf("missing required parameter: baseUrl")
	}
	apiKey := strings.TrimSpace(getStringArg(args, "apiKey", defaults.APIKey))
	if apiKey == "" {
		return nil, fmt.Errorf("missing required parameter: apiKey")
	}

	timeoutSeconds, ok, err := getOptionalIntArg(args, "timeoutSeconds")
	if err != nil {
		return nil, err
	}
	if !ok {
		timeoutSeconds = 30
	}
	if timeoutSeconds < 1 || timeoutSeconds > 120 {
		return nil, fmt.Errorf("timeoutSeconds must be between 1 and 120")
	}

	insecureSkipVerify, ok, err := getOptionalBoolArg(args, "insecureSkipVerify")
	if err != nil {
		return nil, err
	}
	if !ok && defaults.InsecureSkipVerify != nil {
		insecureSkipVerify = *defaults.InsecureSkipVerify
	}

	return unifi.NewClient(unifi.Config{
		BaseURL:            baseURL,
		APIKey:             apiKey,
		Timeout:            time.Duration(timeoutSeconds) * time.Second,
		InsecureSkipVerify: insecureSkipVerify,
	})
}

func buildPath(endpoint unifi.Endpoint, args map[string]interface{}) (string, error) {
	path := endpoint.Path
	for _, param := range endpoint.PathParams {
		value, _, err := getParameterTextArg(args, param, endpoint.PathParameterSchema(param), true)
		if err != nil {
			if param == "siteId" {
				return "", fmt.Errorf("invalid siteId: %w. %s Correct the arguments before retrying", err, unifi.SiteIDGuidance)
			}
			return "", err
		}
		path = strings.ReplaceAll(path, "{"+param+"}", url.PathEscape(value))
	}
	if strings.Contains(path, "{") || strings.Contains(path, "}") {
		return "", fmt.Errorf("unresolved path template in %s", endpoint.Path)
	}
	return path, nil
}

func buildQuery(endpoint unifi.Endpoint, args map[string]interface{}) (url.Values, error) {
	query := url.Values{}
	if endpoint.SupportsPagination {
		offset, ok, err := getOptionalIntArg(args, "offset")
		if err != nil {
			return nil, err
		}
		if ok {
			if offset < 0 {
				return nil, fmt.Errorf("offset must be >= 0")
			}
			query.Set("offset", strconv.Itoa(offset))
		}

		limit, ok, err := getOptionalIntArg(args, "limit")
		if err != nil {
			return nil, err
		}
		if ok {
			maximumLimit := endpoint.PaginationMaximumLimit()
			if limit < 0 || limit > maximumLimit {
				return nil, fmt.Errorf("limit must be between 0 and %d", maximumLimit)
			}
			query.Set("limit", strconv.Itoa(limit))
		}
	}

	if endpoint.SupportsFilter {
		filter := strings.TrimSpace(getStringArg(args, "filter", ""))
		if filter != "" {
			query.Set("filter", filter)
		}
	}

	for _, param := range endpoint.QueryParams {
		schema := param.Schema
		if strings.TrimSpace(schema.Type) == "" {
			schema.Type = "string"
		}
		value, supplied, err := getParameterTextArg(args, param.Name, schema, param.Required)
		if err != nil {
			return nil, err
		}
		if !supplied {
			continue
		}
		query.Set(param.Name, value)
	}

	return query, nil
}

func buildRequestBody(endpoint unifi.Endpoint, args map[string]interface{}) (any, error) {
	value, supplied := args["body"]
	if endpoint.RequestBody == nil {
		if supplied {
			return nil, fmt.Errorf("body is not accepted for %s %s", endpoint.Method, endpoint.Path)
		}
		return nil, nil
	}
	if !supplied || value == nil {
		if endpoint.RequestBody.Required {
			return nil, fmt.Errorf("missing required parameter: body")
		}
		return nil, nil
	}
	if _, ok := value.(map[string]interface{}); !ok {
		return nil, fmt.Errorf("body must be a JSON object")
	}
	return value, nil
}

func getParameterTextArg(args map[string]interface{}, key string, schema unifi.ParameterSchema, required bool) (string, bool, error) {
	switch strings.ToLower(strings.TrimSpace(schema.Type)) {
	case "", "string":
		value, supplied := args[key]
		if !supplied || value == nil {
			if required {
				return "", false, fmt.Errorf("missing required parameter: %s", key)
			}
			return "", false, nil
		}
		text, ok := value.(string)
		if !ok {
			return "", true, fmt.Errorf("%s must be a string", key)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			if required {
				return "", true, fmt.Errorf("missing required parameter: %s", key)
			}
			return "", false, nil
		}
		if schema.Format == "uuid" && (len(text) != 36 || uuid.Validate(text) != nil) {
			return "", true, fmt.Errorf("%s must be a UUID in xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx form", key)
		}
		return text, true, nil
	case "integer":
		value, supplied, err := getOptionalIntArg(args, key)
		if err != nil {
			return "", supplied, err
		}
		if !supplied {
			if required {
				return "", false, fmt.Errorf("missing required parameter: %s", key)
			}
			return "", false, nil
		}
		if schema.Minimum != nil && float64(value) < *schema.Minimum {
			return "", true, fmt.Errorf("%s must be >= %v", key, *schema.Minimum)
		}
		if schema.Maximum != nil && float64(value) > *schema.Maximum {
			return "", true, fmt.Errorf("%s must be <= %v", key, *schema.Maximum)
		}
		return strconv.Itoa(value), true, nil
	case "boolean":
		value, supplied, err := getOptionalBoolArg(args, key)
		if err != nil {
			return "", supplied, err
		}
		if !supplied {
			if required {
				return "", false, fmt.Errorf("missing required parameter: %s", key)
			}
			return "", false, nil
		}
		return strconv.FormatBool(value), true, nil
	default:
		return "", false, fmt.Errorf("unsupported parameter type %q for %s", schema.Type, key)
	}
}

func getStringArg(args map[string]interface{}, key string, defaultValue string) string {
	val, ok := args[key].(string)
	if !ok {
		return defaultValue
	}
	return val
}

func getOptionalBoolArg(args map[string]interface{}, key string) (bool, bool, error) {
	val, ok := args[key]
	if !ok || val == nil {
		return false, false, nil
	}
	boolVal, ok := val.(bool)
	if !ok {
		return false, true, fmt.Errorf("%s must be a boolean", key)
	}
	return boolVal, true, nil
}

func getOptionalIntArg(args map[string]interface{}, key string) (int, bool, error) {
	val, ok := args[key]
	if !ok || val == nil {
		return 0, false, nil
	}
	switch v := val.(type) {
	case int:
		return v, true, nil
	case int32:
		return int(v), true, nil
	case int64:
		return int(v), true, nil
	case float64:
		if math.Trunc(v) != v {
			return 0, true, fmt.Errorf("%s must be an integer", key)
		}
		return int(v), true, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return 0, false, nil
		}
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return 0, true, fmt.Errorf("%s must be an integer", key)
		}
		return parsed, true, nil
	default:
		return 0, true, fmt.Errorf("%s must be an integer", key)
	}
}
