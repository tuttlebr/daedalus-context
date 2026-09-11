package unifi

import "strings"

// SiteIDGuidance explains discovery and the distinction between integration
// API UUIDs and legacy controller site references.
const SiteIDGuidance = "Call listSites first and wait for its result, then use the selected site's UUID from data[].id. Do not use name or internalReference (such as default) as siteId."

// Endpoint describes a UniFi Network integration API operation exposed as an
// MCP tool.
type Endpoint struct {
	Name               string
	Description        string
	Method             string
	Path               string
	PathParams         []string
	PathParamSchemas   map[string]ParameterSchema
	SupportsPagination bool
	SupportsFilter     bool
	PaginationDefault  int
	PaginationMaximum  int
	QueryParams        []QueryParameter
	RequestBody        *RequestBody
	Destructive        bool
}

// ParameterSchema describes the JSON Schema subset used by documented path
// and query parameters.
type ParameterSchema struct {
	Type        string
	Format      string
	Description string
	Default     any
	Minimum     *float64
	Maximum     *float64
}

// QueryParameter describes an additional documented query parameter.
// Pagination and filter parameters use the dedicated Endpoint fields above.
type QueryParameter struct {
	Name        string
	Description string
	Required    bool
	Schema      ParameterSchema
}

// RequestBody is the self-contained JSON Schema for a documented request
// body. Definitions are attached at the MCP input-schema root so local $refs
// resolve without shipping unrelated OpenAPI components.
type RequestBody struct {
	Required    bool
	Description string
	Schema      map[string]any
	Definitions map[string]any
}

const (
	defaultPaginationLimit = 25
	maximumPaginationLimit = 200
)

// PaginationDefaultLimit returns the documented default page size.
func (e Endpoint) PaginationDefaultLimit() int {
	if e.PaginationDefault > 0 {
		return e.PaginationDefault
	}
	return defaultPaginationLimit
}

// PaginationMaximumLimit returns the documented maximum page size.
func (e Endpoint) PaginationMaximumLimit() int {
	if e.PaginationMaximum > 0 {
		return e.PaginationMaximum
	}
	return maximumPaginationLimit
}

// IsReadOnly reports whether the operation is safe to expose as read-only MCP
// functionality.
func (e Endpoint) IsReadOnly() bool {
	return strings.EqualFold(e.Method, methodGET)
}

// IsIdempotent reports the HTTP method's documented retry semantics. POST and
// PATCH are treated conservatively; GET, PUT, and DELETE are idempotent.
func (e Endpoint) IsIdempotent() bool {
	switch strings.ToUpper(e.Method) {
	case methodGET, methodPUT, methodDELETE:
		return true
	default:
		return false
	}
}

// PathParameterSchema returns the documented schema for a path parameter.
// Existing read-only endpoints use string identifiers unless overridden.
func (e Endpoint) PathParameterSchema(name string) ParameterSchema {
	if schema, ok := e.PathParamSchemas[name]; ok {
		return schema.withDefaults()
	}
	return ParameterSchema{Type: "string"}
}

func (s ParameterSchema) withDefaults() ParameterSchema {
	if strings.TrimSpace(s.Type) == "" {
		s.Type = "string"
	}
	return s
}

// Endpoints returns the complete MCP surface. The stable read-only aliases are
// curated below, while mutation operations and their request schemas are
// loaded from the embedded, checksum-pinned OpenAPI contract.
func Endpoints() []Endpoint {
	out := make([]Endpoint, len(endpoints))
	copy(out, endpoints)
	return out
}

// EndpointByName finds a registered endpoint by MCP tool name.
func EndpointByName(name string) (Endpoint, bool) {
	for _, endpoint := range endpoints {
		if endpoint.Name == name {
			return endpoint, true
		}
	}
	return Endpoint{}, false
}

const (
	methodGET    = "GET"
	methodPOST   = "POST"
	methodPUT    = "PUT"
	methodPATCH  = "PATCH"
	methodDELETE = "DELETE"
)

var readOnlyEndpoints = []Endpoint{
	{
		Name:        "getInfo",
		Description: "Retrieve general information about the UniFi Network application.",
		Method:      methodGET,
		Path:        "/v1/info",
	},
	{
		Name:               "listSites",
		Description:        "Retrieve a paginated list of local sites managed by this Network application. " + SiteIDGuidance,
		Method:             methodGET,
		Path:               "/v1/sites",
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:               "listAdoptedDevices",
		Description:        "List adopted UniFi devices for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/devices",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:        "getAdoptedDeviceDetails",
		Description: "Get detailed information for an adopted UniFi device.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/devices/{deviceId}",
		PathParams:  []string{"siteId", "deviceId"},
	},
	{
		Name:        "getAdoptedDeviceLatestStatistics",
		Description: "Get the latest statistics for an adopted UniFi device.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/devices/{deviceId}/statistics/latest",
		PathParams:  []string{"siteId", "deviceId"},
	},
	{
		Name:               "listPendingDevices",
		Description:        "List UniFi devices pending adoption.",
		Method:             methodGET,
		Path:               "/v1/pending-devices",
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:               "listConnectedClients",
		Description:        "List connected clients for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/clients",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:        "getConnectedClientDetails",
		Description: "Get detailed information for a connected client.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/clients/{clientId}",
		PathParams:  []string{"siteId", "clientId"},
	},
	{
		Name:               "listNetworks",
		Description:        "List configured networks for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/networks",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:        "getNetworkDetails",
		Description: "Get details for a configured network.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/networks/{networkId}",
		PathParams:  []string{"siteId", "networkId"},
	},
	{
		Name:        "getNetworkReferences",
		Description: "Get objects that reference a configured network.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/networks/{networkId}/references",
		PathParams:  []string{"siteId", "networkId"},
	},
	{
		Name:               "listWifiBroadcasts",
		Description:        "List Wi-Fi broadcasts for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/wifi/broadcasts",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:        "getWifiBroadcastDetails",
		Description: "Get details for a Wi-Fi broadcast.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/wifi/broadcasts/{wifiBroadcastId}",
		PathParams:  []string{"siteId", "wifiBroadcastId"},
	},
	{
		Name:               "listVouchers",
		Description:        "List hotspot vouchers for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/hotspot/vouchers",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
		PaginationDefault:  100,
		PaginationMaximum:  1000,
	},
	{
		Name:        "getVoucherDetails",
		Description: "Get details for a hotspot voucher.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/hotspot/vouchers/{voucherId}",
		PathParams:  []string{"siteId", "voucherId"},
	},
	{
		Name:               "listFirewallZones",
		Description:        "List firewall zones for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/firewall/zones",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:        "getFirewallZone",
		Description: "Get a firewall zone by id.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/firewall/zones/{firewallZoneId}",
		PathParams:  []string{"siteId", "firewallZoneId"},
	},
	{
		Name:               "listFirewallPolicies",
		Description:        "List firewall policies for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/firewall/policies",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:        "getFirewallPolicy",
		Description: "Get a firewall policy by id.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/firewall/policies/{firewallPolicyId}",
		PathParams:  []string{"siteId", "firewallPolicyId"},
	},
	{
		Name:        "getFirewallPolicyOrdering",
		Description: "Get user-defined firewall policy ordering for a source and destination firewall zone pair.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/firewall/policies/ordering",
		PathParams:  []string{"siteId"},
		QueryParams: []QueryParameter{
			{
				Name:        "sourceFirewallZoneId",
				Description: "Required source firewall zone UUID for the policy ordering.",
				Required:    true,
			},
			{
				Name:        "destinationFirewallZoneId",
				Description: "Required destination firewall zone UUID for the policy ordering.",
				Required:    true,
			},
		},
	},
	{
		Name:               "listAclRules",
		Description:        "List access-control rules for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/acl-rules",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:        "getAclRule",
		Description: "Get an access-control rule by id.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/acl-rules/{aclRuleId}",
		PathParams:  []string{"siteId", "aclRuleId"},
	},
	{
		Name:        "getAclRuleOrdering",
		Description: "Get user-defined access-control rule ordering for a site.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/acl-rules/ordering",
		PathParams:  []string{"siteId"},
	},
	{
		Name:               "listSwitchStacks",
		Description:        "List switch stacks for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/switching/switch-stacks",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:        "getSwitchStack",
		Description: "Get a switch stack by id.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/switching/switch-stacks/{switchStackId}",
		PathParams:  []string{"siteId", "switchStackId"},
	},
	{
		Name:               "listMcLagDomains",
		Description:        "List MC-LAG domains for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/switching/mc-lag-domains",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:        "getMcLagDomain",
		Description: "Get an MC-LAG domain by id.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/switching/mc-lag-domains/{mcLagDomainId}",
		PathParams:  []string{"siteId", "mcLagDomainId"},
	},
	{
		Name:               "listLags",
		Description:        "List link aggregation groups for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/switching/lags",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:        "getLagDetails",
		Description: "Get a link aggregation group by id.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/switching/lags/{lagId}",
		PathParams:  []string{"siteId", "lagId"},
	},
	{
		Name:               "listDnsPolicies",
		Description:        "List DNS policies for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/dns/policies",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:        "getDnsPolicy",
		Description: "Get a DNS policy by id.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/dns/policies/{dnsPolicyId}",
		PathParams:  []string{"siteId", "dnsPolicyId"},
	},
	{
		Name:               "listTrafficMatchingLists",
		Description:        "List traffic matching lists for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/traffic-matching-lists",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:        "getTrafficMatchingList",
		Description: "Get a traffic matching list by id.",
		Method:      methodGET,
		Path:        "/v1/sites/{siteId}/traffic-matching-lists/{trafficMatchingListId}",
		PathParams:  []string{"siteId", "trafficMatchingListId"},
	},
	{
		Name:               "listWanInterfaces",
		Description:        "List WAN interfaces for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/wans",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
	},
	{
		Name:               "listSiteToSiteVpnTunnels",
		Description:        "List site-to-site VPN tunnels for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/vpn/site-to-site-tunnels",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:               "listVpnServers",
		Description:        "List VPN servers for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/vpn/servers",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:               "listRadiusProfiles",
		Description:        "List RADIUS profiles for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/radius/profiles",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:               "listDeviceTags",
		Description:        "List device tags for a site.",
		Method:             methodGET,
		Path:               "/v1/sites/{siteId}/device-tags",
		PathParams:         []string{"siteId"},
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:               "listDpiApplicationCategories",
		Description:        "List DPI application categories.",
		Method:             methodGET,
		Path:               "/v1/dpi/categories",
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:               "listDpiApplications",
		Description:        "List DPI applications.",
		Method:             methodGET,
		Path:               "/v1/dpi/applications",
		SupportsPagination: true,
		SupportsFilter:     true,
	},
	{
		Name:               "listCountries",
		Description:        "List countries known to UniFi Network.",
		Method:             methodGET,
		Path:               "/v1/countries",
		SupportsPagination: true,
		SupportsFilter:     true,
	},
}

var endpoints = mustBuildEndpoints(readOnlyEndpoints)
