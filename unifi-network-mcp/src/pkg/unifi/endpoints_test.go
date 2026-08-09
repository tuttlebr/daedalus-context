package unifi

import "testing"

func TestEndpointsAreReadOnlyGETs(t *testing.T) {
	endpoints := Endpoints()
	if len(endpoints) != 41 {
		t.Fatalf("endpoint count = %d, want 41", len(endpoints))
	}

	seen := map[string]bool{}
	for _, endpoint := range endpoints {
		if endpoint.Method != methodGET {
			t.Fatalf("%s method = %s, want GET", endpoint.Name, endpoint.Method)
		}
		if seen[endpoint.Name] {
			t.Fatalf("duplicate endpoint name %q", endpoint.Name)
		}
		seen[endpoint.Name] = true
	}

	for _, forbidden := range []string{
		"adoptDevice",
		"removeDevice",
		"createNetwork",
		"updateNetwork",
		"deleteNetwork",
		"executeConnectedClientAction",
		"executeAdoptedDeviceAction",
	} {
		if _, ok := EndpointByName(forbidden); ok {
			t.Fatalf("mutation endpoint %q must not be registered", forbidden)
		}
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
