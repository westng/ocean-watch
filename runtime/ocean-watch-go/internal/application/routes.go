package application

import (
	"fmt"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
)

type Runtime string

const (
	RuntimeGo Runtime = "go"
)

type RouteManifest struct {
	version int
	routes  map[string]Runtime
}

func NewRouteManifest(version int, routes map[string]Runtime) (RouteManifest, error) {
	copyOfRoutes := make(map[string]Runtime, len(routes))
	for _, name := range contracts.Names() {
		runtime, ok := routes[name]
		if !ok {
			return RouteManifest{}, fmt.Errorf("route is missing for command %s", name)
		}
		if runtime != RuntimeGo {
			return RouteManifest{}, fmt.Errorf("route is invalid for command %s", name)
		}
		copyOfRoutes[name] = runtime
	}
	if len(copyOfRoutes) != len(routes) {
		return RouteManifest{}, fmt.Errorf("route manifest contains unknown commands")
	}
	return RouteManifest{version: version, routes: copyOfRoutes}, nil
}

func DefaultRouteManifest() RouteManifest {
	routes := make(map[string]Runtime, len(contracts.Commands))
	for _, command := range contracts.Commands {
		routes[command.Name()] = RuntimeGo
	}
	manifest, err := NewRouteManifest(6, routes)
	if err != nil {
		panic(err)
	}
	return manifest
}

func ProductionRouteManifest() RouteManifest {
	return DefaultRouteManifest()
}

func (manifest RouteManifest) Version() int { return manifest.version }

func (manifest RouteManifest) RouteFor(name string) (Runtime, bool) {
	runtime, ok := manifest.routes[name]
	return runtime, ok
}

func (manifest RouteManifest) Snapshot() map[string]Runtime {
	result := make(map[string]Runtime, len(manifest.routes))
	for command, runtime := range manifest.routes {
		result[command] = runtime
	}
	return result
}
