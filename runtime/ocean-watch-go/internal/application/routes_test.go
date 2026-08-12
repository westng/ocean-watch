package application

import "testing"

func TestRouteManifestIsImmutableAndAllGo(t *testing.T) {
	routes := DefaultRouteManifest()
	snapshot := routes.Snapshot()
	snapshot["accounts list"] = "invalid"
	for command, runtime := range routes.Snapshot() {
		if runtime != RuntimeGo {
			t.Fatalf("route %s = %s, want go", command, runtime)
		}
	}
}

func TestProductionRouteManifestMatchesDefault(t *testing.T) {
	if DefaultRouteManifest().Version() != 6 || ProductionRouteManifest().Version() != 6 {
		t.Fatal("single Go runtime manifest version changed")
	}
	if len(DefaultRouteManifest().Snapshot()) != len(ProductionRouteManifest().Snapshot()) {
		t.Fatal("production and default route manifests differ")
	}
}
