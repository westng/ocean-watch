package main

import (
	"encoding/json"
	"flag"
	"os"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
)

func main() {
	candidate := flag.Bool("candidate", false, "emit isolated candidate Shadow routes")
	flag.Parse()
	manifest := application.ProductionRouteManifest()
	if *candidate {
		manifest = application.DefaultRouteManifest()
	}
	routes := make(map[string]string, len(manifest.Snapshot())+1)
	for command, runtime := range manifest.Snapshot() {
		routes[command] = string(runtime)
	}
	routes["--version"] = "python"
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"schema_version":         1,
		"route_manifest_version": manifest.Version(),
		"routes":                 routes,
	}); err != nil {
		panic(err)
	}
}
