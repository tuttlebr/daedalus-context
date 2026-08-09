package unifi

// Endpoint describes a read-only UniFi Network integration API operation
// exposed as an MCP tool.
type Endpoint struct {
	Name               string
	Description        string
	Method             string
	Path               string
	PathParams         []string
	SupportsPagination bool
	SupportsFilter     bool
	PaginationDefault  int
	PaginationMaximum  int
	QueryParams        []QueryParameter
}

// QueryParameter describes an additional documented string query parameter.
// Pagination and filter parameters use the dedicated Endpoint fields above.
type QueryParameter struct {
	Name        string
	Description string
	Required    bool
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

// Endpoints returns the complete hardcoded read-only MCP surface. Every entry
// maps to a documented GET endpoint from the UniFi Network 10.4.57 API capture.
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

const methodGET = "GET"

var endpoints = []Endpoint{
	{
		Name:        "getInfo",
		Description: "Retrieve general information about the UniFi Network application.",
		Method:      methodGET,
		Path:        "/v1/info",
	},
	{
		Name:               "listSites",
		Description:        "Retrieve a paginated list of local sites managed by this Network application. Use a site id from this response for site-scoped tools.",
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
