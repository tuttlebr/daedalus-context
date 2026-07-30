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

// EndpointTool creates an MCP tool schema for a read-only UniFi endpoint.
func EndpointTool(endpoint unifi.Endpoint, defaults ToolDefaults) mcp.Tool {
	description := endpoint.Description
	if defaults.ConnectionLocked {
		description += " Uses the deployment's fixed UniFi controller and server-side credential."
	} else {
		description += " The client may provide baseUrl and apiKey for this read-only request."
	}
	if !defaults.ConnectionLocked && (defaults.HasBaseURLDefault || defaults.HasAPIKeyDefault) {
		description += " Configured server-side defaults are used for omitted connection fields."
	}

	options := []mcp.ToolOption{
		mcp.WithDescription(description),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithNumber("timeoutSeconds",
			mcp.Min(1),
			mcp.Max(120),
			mcp.Description("Optional outbound UniFi API timeout for this request in seconds. Default: 30."),
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
		options = append(options, mcp.WithString(
			param,
			mcp.Required(),
			mcp.Description(fmt.Sprintf("Required path parameter %q for %s.", param, endpoint.Path)),
		))
	}

	if endpoint.SupportsPagination {
		options = append(options,
			mcp.WithNumber("offset",
				mcp.Min(0),
				mcp.Description("Optional pagination offset. UniFi default is 0."),
			),
			mcp.WithNumber("limit",
				mcp.Min(0),
				mcp.Max(200),
				mcp.Description("Optional pagination limit. UniFi default is 25; documented maximum is 200."),
			),
		)
	}

	if endpoint.SupportsFilter {
		options = append(options, mcp.WithString(
			"filter",
			mcp.Description("Optional UniFi filter expression, for example name.eq('Default') or and(name.isNotNull(),createdAt.gt(2025-01-01))."),
		))
	}

	return mcp.NewTool(endpoint.Name, options...)
}

// EndpointTools returns MCP tool schemas for every read-only UniFi endpoint.
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
