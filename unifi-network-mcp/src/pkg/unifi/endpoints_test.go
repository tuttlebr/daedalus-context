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
