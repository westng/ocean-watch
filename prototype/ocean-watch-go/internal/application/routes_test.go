package application

import "testing"

func TestRouteManifestIsImmutable(t *testing.T) {
	routes := DefaultRouteManifest()
	snapshot := routes.Snapshot()
	snapshot["accounts list"] = RuntimePython
	runtime, ok := routes.RouteFor("accounts list")
	if !ok || runtime != RuntimeGo {
		t.Fatal("snapshot mutation changed route manifest")
	}
}

func TestDefaultRouteOnlyMigratesApprovedLocalSlice(t *testing.T) {
	routes := DefaultRouteManifest()
	for command, runtime := range routes.Snapshot() {
		approved := command == "setup doctor" || command == "setup init" ||
			command == "setup validate" ||
			command == "auth migrate" ||
			command == "accounts list" || command == "accounts add" ||
			command == "accounts remove" || command == "accounts enable" ||
			command == "accounts disable" || command == "runs list" ||
			command == "runs show" || command == "templates list" ||
			command == "templates show" || command == "templates migrate" ||
			command == "templates create" || command == "templates set-copy" ||
			command == "templates validate" || command == "templates delete" ||
			command == "qc-templates list" || command == "qc-templates create" ||
			command == "qc-templates migrate" || command == "qc-templates list-live" ||
			command == "qc-templates create-live" || command == "qc-templates migrate-live"
		if approved && runtime != RuntimeGo {
			t.Fatalf("approved command is not Go: %s", command)
		}
		if !approved && runtime != RuntimePython {
			t.Fatalf("unapproved command escaped Python fallback: %s", command)
		}
	}
}

func TestDefaultRouteManifestVersion(t *testing.T) {
	if version := DefaultRouteManifest().Version(); version != 5 {
		t.Fatalf("route manifest version = %d, want 5", version)
	}
}

func TestProductionRouteManifestKeepsEveryCommandOnPython(t *testing.T) {
	manifest := ProductionRouteManifest()
	for command, runtime := range manifest.Snapshot() {
		if runtime != RuntimePython {
			t.Fatalf("production route %s = %s, want python", command, runtime)
		}
	}
}
