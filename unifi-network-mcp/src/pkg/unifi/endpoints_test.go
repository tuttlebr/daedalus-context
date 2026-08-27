package unifi

import "testing"

func TestEndpointsExposeCompleteOperationSet(t *testing.T) {
	registered := Endpoints()
	if len(registered) != 73 {
		t.Fatalf("endpoint count = %d, want 73", len(registered))
	}

	methodCounts := map[string]int{}
	seen := map[string]bool{}
	for _, endpoint := range registered {
		if !isSupportedMethod(endpoint.Method) {
			t.Fatalf("%s uses unsupported method %s", endpoint.Name, endpoint.Method)
		}
		if seen[endpoint.Name] {
			t.Fatalf("duplicate endpoint name %q", endpoint.Name)
		}
		seen[endpoint.Name] = true
		methodCounts[endpoint.Method]++
	}

	wantCounts := map[string]int{
		methodGET:    41,
		methodPOST:   12,
		methodPUT:    9,
		methodPATCH:  1,
		methodDELETE: 10,
	}
	for method, want := range wantCounts {
		if got := methodCounts[method]; got != want {
			t.Errorf("%s endpoint count = %d, want %d", method, got, want)
		}
	}

	for _, required := range []string{
		"listSites",
		"adoptDevice",
		"removeDevice",
		"createNetwork",
		"updateNetwork",
		"deleteNetwork",
		"executeConnectedClientAction",
		"executeAdoptedDeviceAction",
		"patchFirewallPolicy",
	} {
		if _, ok := EndpointByName(required); !ok {
			t.Errorf("required endpoint %q is not registered", required)
		}
	}
}

func TestEndpointMutationMetadata(t *testing.T) {
	tests := []struct {
		name        string
		readOnly    bool
		destructive bool
		idempotent  bool
		hasBody     bool
	}{
		{name: "listSites", readOnly: true, idempotent: true},
		{name: "createNetwork", hasBody: true},
		{name: "adoptDevice", destructive: true, hasBody: true},
		{name: "executeConnectedClientAction", destructive: true, hasBody: true},
		{name: "updateNetwork", destructive: true, idempotent: true, hasBody: true},
		{name: "patchFirewallPolicy", destructive: true, hasBody: true},
		{name: "deleteNetwork", destructive: true, idempotent: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			endpoint, ok := EndpointByName(test.name)
			if !ok {
				t.Fatalf("endpoint %s missing", test.name)
			}
			if endpoint.IsReadOnly() != test.readOnly {
				t.Errorf("readOnly = %t, want %t", endpoint.IsReadOnly(), test.readOnly)
			}
			if endpoint.Destructive != test.destructive {
				t.Errorf("destructive = %t, want %t", endpoint.Destructive, test.destructive)
			}
			if endpoint.IsIdempotent() != test.idempotent {
				t.Errorf("idempotent = %t, want %t", endpoint.IsIdempotent(), test.idempotent)
			}
			if (endpoint.RequestBody != nil) != test.hasBody {
				t.Errorf("hasBody = %t, want %t", endpoint.RequestBody != nil, test.hasBody)
			}
		})
	}
}

func TestEndpointMutationParameterTypes(t *testing.T) {
	portAction, ok := EndpointByName("executePortAction")
	if !ok {
		t.Fatal("executePortAction endpoint missing")
	}
	if got := portAction.PathParameterSchema("portIdx").Type; got != "integer" {
		t.Fatalf("portIdx type = %q, want integer", got)
	}

	deleteNetwork, ok := EndpointByName("deleteNetwork")
	if !ok {
		t.Fatal("deleteNetwork endpoint missing")
	}
	force := findQueryParameter(t, deleteNetwork, "force")
	if force.Required {
		t.Fatal("deleteNetwork force must be optional")
	}
	if force.Schema.Type != "boolean" || force.Schema.Default != false {
		t.Fatalf("force schema = %#v, want optional boolean default false", force.Schema)
	}

	deleteVouchers, ok := EndpointByName("deleteVouchers")
	if !ok {
		t.Fatal("deleteVouchers endpoint missing")
	}
	filter := findQueryParameter(t, deleteVouchers, "filter")
	if !filter.Required || filter.Schema.Type != "string" {
		t.Fatalf("deleteVouchers filter = %#v, want required string", filter)
	}
}

func TestEndpointPaginationMatchesDocumentedExceptions(t *testing.T) {
	vouchers, ok := EndpointByName("listVouchers")
	if !ok {
		t.Fatal("listVouchers endpoint missing")
	}
	if got := vouchers.PaginationDefaultLimit(); got != 100 {
		t.Fatalf("voucher default limit = %d, want 100", got)
	}
	if got := vouchers.PaginationMaximumLimit(); got != 1000 {
		t.Fatalf("voucher maximum limit = %d, want 1000", got)
	}

	wans, ok := EndpointByName("listWanInterfaces")
	if !ok {
		t.Fatal("listWanInterfaces endpoint missing")
	}
	if wans.SupportsFilter {
		t.Fatal("WAN interfaces must not advertise an undocumented filter")
	}
}

func findQueryParameter(t *testing.T, endpoint Endpoint, name string) QueryParameter {
	t.Helper()
	for _, parameter := range endpoint.QueryParams {
		if parameter.Name == name {
			return parameter
		}
	}
	t.Fatalf("%s query parameter %s missing", endpoint.Name, name)
	return QueryParameter{}
}
