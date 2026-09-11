package tools

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tuttlebr/unifi-network-mcp/pkg/unifi"
)

// ToolDefaults describes which connection values are configured server-side
// so MCP schemas can avoid requiring redundant per-call inputs.
type ToolDefaults struct {
	HasBaseURLDefault            bool
	HasAPIKeyDefault             bool
	InsecureSkipVerifyDefaultSet bool
	InsecureSkipVerifyDefault    bool
	ConnectionLocked             bool
}

// EndpointTool creates an MCP tool schema for a UniFi endpoint.
func EndpointTool(endpoint unifi.Endpoint, defaults ToolDefaults) mcp.Tool {
	description := endpoint.Description
	if defaults.ConnectionLocked {
		description += " Uses the deployment's fixed UniFi controller and server-side credential."
	} else {
		description += " The client may provide baseUrl and apiKey for this request."
	}
	if !defaults.ConnectionLocked && (defaults.HasBaseURLDefault || defaults.HasAPIKeyDefault) {
		description += " Configured server-side defaults are used for omitted connection fields."
	}
	if endpoint.Destructive {
		description += " This operation changes or removes controller-managed state; review the arguments before confirming the tool call."
	}

	options := []mcp.ToolOption{
		mcp.WithDescription(description),
		mcp.WithReadOnlyHintAnnotation(endpoint.IsReadOnly()),
		mcp.WithDestructiveHintAnnotation(endpoint.Destructive),
		mcp.WithIdempotentHintAnnotation(endpoint.IsIdempotent()),
		mcp.WithOpenWorldHintAnnotation(true),
		withInteger(
			"timeoutSeconds",
			"Optional outbound UniFi API timeout for this request in seconds. Default: 30.",
			1,
			120,
		),
	}

	if !defaults.ConnectionLocked {
		baseURLOpts := []mcp.PropertyOption{
			mcp.Description(connectionDescription(
				"UniFi Network local integration API base URL for this request, for example https://<unifi-console>/proxy/network/integration.",
				defaults.HasBaseURLDefault,
			)),
		}
		if !defaults.HasBaseURLDefault {
			baseURLOpts = append(baseURLOpts, mcp.Required())
		}
		options = append(options, mcp.WithString("baseUrl", baseURLOpts...))

		apiKeyOpts := []mcp.PropertyOption{
			mcp.Description(connectionDescription(
				"UniFi Network integration API key for this request. This value is used only for the outbound X-API-KEY header and is never logged by the server.",
				defaults.HasAPIKeyDefault,
			)),
		}
		if !defaults.HasAPIKeyDefault {
			apiKeyOpts = append(apiKeyOpts, mcp.Required())
		}
		options = append(options, mcp.WithString("apiKey", apiKeyOpts...))

		insecureDescription := "Set true only when connecting to a local UniFi console with a self-signed or otherwise untrusted TLS certificate."
		insecureOpts := []mcp.PropertyOption{mcp.Description(insecureDescription + " Default: false.")}
		if defaults.InsecureSkipVerifyDefaultSet {
			insecureOpts = []mcp.PropertyOption{
				mcp.Description(insecureDescription + " Defaults to the server-side setting when omitted."),
				mcp.DefaultBool(defaults.InsecureSkipVerifyDefault),
			}
		}
		options = append(options, mcp.WithBoolean("insecureSkipVerify", insecureOpts...))
	}

	for _, param := range endpoint.PathParams {
		schema := endpoint.PathParameterSchema(param)
		if param == "siteId" {
			schema.Description = "Required site UUID. " + unifi.SiteIDGuidance
		} else if schema.Description == "" {
			schema.Description = fmt.Sprintf("Required path parameter %q for %s.", param, endpoint.Path)
		}
		options = append(options, withParameter(param, schema, true))
	}

	if endpoint.SupportsPagination {
		defaultLimit := endpoint.PaginationDefaultLimit()
		maximumLimit := endpoint.PaginationMaximumLimit()
		options = append(options,
			withInteger(
				"offset",
				"Optional pagination offset. UniFi default is 0.",
				0,
				0,
				"int32",
			),
			withInteger(
				"limit",
				fmt.Sprintf("Optional pagination limit. UniFi default is %d; documented maximum is %d.", defaultLimit, maximumLimit),
				0,
				maximumLimit,
				"int32",
			),
		)
	}

	if endpoint.SupportsFilter {
		options = append(options, mcp.WithString(
			"filter",
			mcp.Description("Optional UniFi filter expression, for example name.eq('Default') or and(name.isNotNull(),createdAt.gt(2025-01-01))."),
		))
	}

	for _, param := range endpoint.QueryParams {
		schema := param.Schema
		if schema.Description == "" {
			schema.Description = param.Description
		}
		if schema.Description == "" {
			schema.Description = fmt.Sprintf("Documented query parameter %q for %s.", param.Name, endpoint.Path)
		}
		options = append(options, withParameter(param.Name, schema, param.Required))
	}
	if endpoint.RequestBody != nil {
		options = append(options, withRequestBody(endpoint.RequestBody))
	}

	return mcp.NewTool(endpoint.Name, options...)
}

func withParameter(name string, parameter unifi.ParameterSchema, required bool) mcp.ToolOption {
	return func(tool *mcp.Tool) {
		parameterType := strings.TrimSpace(parameter.Type)
		if parameterType == "" {
			parameterType = "string"
		}
		schema := map[string]any{
			"type": parameterType,
		}
		if parameter.Format != "" {
			schema["format"] = parameter.Format
		}
		if parameter.Description != "" {
			schema["description"] = parameter.Description
		}
		if parameter.Default != nil {
			schema["default"] = parameter.Default
		}
		if parameter.Minimum != nil {
			schema["minimum"] = *parameter.Minimum
		}
		if parameter.Maximum != nil {
			schema["maximum"] = *parameter.Maximum
		}
		tool.InputSchema.Properties[name] = schema
		if required {
			tool.InputSchema.Required = append(tool.InputSchema.Required, name)
		}
	}
}

func withRequestBody(requestBody *unifi.RequestBody) mcp.ToolOption {
	return func(tool *mcp.Tool) {
		schema := make(map[string]any, len(requestBody.Schema)+1)
		for key, value := range requestBody.Schema {
			schema[key] = value
		}
		if _, exists := schema["description"]; !exists && requestBody.Description != "" {
			schema["description"] = requestBody.Description
		}
		tool.InputSchema.Properties["body"] = schema
		if requestBody.Required {
			tool.InputSchema.Required = append(tool.InputSchema.Required, "body")
		}
		if len(requestBody.Definitions) > 0 {
			tool.InputSchema.Defs = requestBody.Definitions
		}
	}
}

// withInteger adds the integer JSON Schema type that mcp-go's convenience
// builders do not currently expose. A maximum of zero means unbounded.
func withInteger(name, description string, minimum, maximum int, formats ...string) mcp.ToolOption {
	return func(tool *mcp.Tool) {
		schema := map[string]any{
			"type":        "integer",
			"description": description,
			"minimum":     minimum,
		}
		if maximum > 0 {
			schema["maximum"] = maximum
		}
		if len(formats) > 0 && formats[0] != "" {
			schema["format"] = formats[0]
		}
		tool.InputSchema.Properties[name] = schema
	}
}

// EndpointTools returns MCP tool schemas for every UniFi endpoint.
func EndpointTools() []mcp.Tool {
	endpoints := unifi.Endpoints()
	result := make([]mcp.Tool, 0, len(endpoints))
	for _, endpoint := range endpoints {
		result = append(result, EndpointTool(endpoint, ToolDefaults{}))
	}
	return result
}

func connectionDescription(base string, hasDefault bool) string {
	if hasDefault {
		return strings.TrimSpace(base + " Optional because a server-side default is configured.")
	}
	return base
}
