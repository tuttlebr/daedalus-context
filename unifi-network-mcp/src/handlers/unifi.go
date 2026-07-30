package handlers

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

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

// EndpointHandler creates an MCP handler for a hardcoded read-only UniFi GET
// endpoint.
func EndpointHandler(endpoint unifi.Endpoint, defaults ClientDefaults) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		client, err := clientFromArgs(args, defaults)
		if err != nil {
			return nil, err
		}

		path, err := buildPath(endpoint, args)
		if err != nil {
			return nil, err
		}

		query, err := buildQuery(endpoint, args)
		if err != nil {
			return nil, err
		}

		data, err := client.GetJSON(ctx, path, query)
		if err != nil {
			return nil, err
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
		value, err := getRequiredStringArg(args, param)
		if err != nil {
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
			if limit < 0 || limit > 200 {
				return nil, fmt.Errorf("limit must be between 0 and 200")
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

	return query, nil
}

func getStringArg(args map[string]interface{}, key string, defaultValue string) string {
	val, ok := args[key].(string)
	if !ok {
		return defaultValue
	}
	return val
}

func getRequiredStringArg(args map[string]interface{}, key string) (string, error) {
	val, ok := args[key].(string)
	if !ok || strings.TrimSpace(val) == "" {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	return val, nil
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
