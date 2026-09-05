package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCommandsAreUniqueAndExcludeRemovedCompatibilityDomains(t *testing.T) {
	seen := map[string]bool{}
	for _, command := range Commands {
		if command.Domain == "" || command.Action == "" || command.Description == "" {
			t.Fatalf("incomplete command: %#v", command)
		}
		if command.Domain == "mcp" {
			t.Fatalf("removed MCP compatibility command remains: %s", command.Name())
		}
		if command.Name() == "auth migrate" || command.Name() == "templates migrate" ||
			command.Name() == "qc-templates migrate" || command.Name() == "qc-templates migrate-live" {
			t.Fatalf("removed migration command remains: %s", command.Name())
		}
		if seen[command.Name()] {
			t.Fatalf("duplicate command: %s", command.Name())
		}
		capability := CapabilityFor(command)
		if capability.Channel == "" || capability.Effect == "" ||
			(capability.Effect == EffectOnlineWrite && !capability.RequiresSubmit) {
			t.Fatalf("invalid command capability: command=%#v capability=%#v", command, capability)
		}
		seen[command.Name()] = true
	}
	if len(Commands) != 76 {
		t.Fatalf("command count = %d, want 76", len(Commands))
	}
}

func TestCapabilityRegistryMapsEveryTransportExactlyOnce(t *testing.T) {
	seenIDs := map[string]bool{}
	seenTools := map[string]bool{}
	seenCommands := map[string]bool{}
	for _, spec := range DefaultCapabilityRegistry.All() {
		if seenIDs[spec.ID] {
			t.Fatalf("duplicate capability id: %s", spec.ID)
		}
		seenIDs[spec.ID] = true
		if spec.MCPTool != "" {
			if seenTools[spec.MCPTool] {
				t.Fatalf("duplicate MCP tool: %s", spec.MCPTool)
			}
			seenTools[spec.MCPTool] = true
		}
		if spec.CLICommand != "" {
			if seenCommands[spec.CLICommand] {
				t.Fatalf("duplicate CLI command: %s", spec.CLICommand)
			}
			seenCommands[spec.CLICommand] = true
		}
		if spec.Effect == EffectOnlineWrite && !spec.RequiresSubmit {
			t.Fatalf("submit contract drift: %#v", spec)
		}
	}
	if len(seenCommands) != 76 || len(seenTools) != 18 {
		t.Fatalf("transport inventory changed: commands=%d tools=%d", len(seenCommands), len(seenTools))
	}
}

func TestLocalPlanBindingWriteRequiresExplicitSubmit(t *testing.T) {
	spec, ok := DefaultCapabilityRegistry.ByCLICommand("qc-plans bind")
	if !ok || spec.Effect != EffectLocalWrite || !spec.RequiresSubmit {
		t.Fatalf("plan binding capability=%#v exists=%t", spec, ok)
	}
}

func TestFastRoutesReferenceRegisteredCapabilities(t *testing.T) {
	seen := map[string]bool{}
	for _, route := range DefaultCapabilityRegistry.FastRoutes() {
		key := route.Skill + "\x00" + route.Outcome
		if seen[key] {
			t.Fatalf("duplicate fast route: %#v", route)
		}
		seen[key] = true
		spec, ok := DefaultCapabilityRegistry.ByID(route.CapabilityID)
		if !ok || !spec.FastRoute {
			t.Fatalf("fast route references non-fast capability: %#v", route)
		}
	}
}

type fastRouteFixture struct {
	Skill        string `json:"skill"`
	Outcome      string `json:"outcome"`
	CapabilityID string `json:"capability_id"`
	Surface      string `json:"surface"`
	Route        string `json:"route"`
}

func TestFastRouteFixtureAndSkillTablesMatchRegistry(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	fixturePath := filepath.Join(filepath.Dir(source), "testdata", "skill-fast-routes.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var fixture []fastRouteFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	routes := DefaultCapabilityRegistry.FastRoutes()
	if len(fixture) != len(routes) {
		t.Fatalf("fast route fixture count=%d registry count=%d", len(fixture), len(routes))
	}
	for index, route := range routes {
		got := fixture[index]
		if got.Skill != route.Skill || got.Outcome != route.Outcome || got.CapabilityID != route.CapabilityID || got.Surface != string(route.Surface) || got.Route != route.Route {
			t.Fatalf("fixture route %d=%#v registry=%#v", index, got, route)
		}
	}
	root := filepath.Join(filepath.Dir(source), "..", "..", "..", "..")
	for _, skill := range []string{"ads-plan-monitor", "qc-plan-monitor"} {
		content, err := os.ReadFile(filepath.Join(root, "skills", skill, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(string(content), "<!-- capability-routes:start -->")
		end := strings.Index(string(content), "<!-- capability-routes:end -->")
		if start < 0 || end <= start {
			t.Fatalf("%s lacks a valid controlled capability route block", skill)
		}
		block := string(content[start:end])
		rows := make([]fastRouteFixture, 0)
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "|") || strings.Contains(line, "User outcome") || strings.Contains(line, "| --- |") {
				continue
			}
			cells := strings.Split(strings.Trim(line, "|"), "|")
			if len(cells) != 2 {
				t.Fatalf("malformed %s route row: %q", skill, line)
			}
			outcome := strings.TrimSpace(cells[0])
			routeCell := strings.TrimSpace(cells[1])
			fixtureRoute := fastRouteFixture{Skill: skill, Outcome: outcome}
			if strings.HasPrefix(routeCell, "MCP `") && strings.HasSuffix(routeCell, "`") {
				fixtureRoute.Surface = string(SurfaceMCP)
				fixtureRoute.Route = strings.TrimSuffix(strings.TrimPrefix(routeCell, "MCP `"), "`")
			} else if strings.HasPrefix(routeCell, "`") && strings.HasSuffix(routeCell, "`") {
				fixtureRoute.Surface = string(SurfaceCLI)
				fixtureRoute.Route = strings.Trim(routeCell, "`")
			} else {
				t.Fatalf("unsupported %s route cell: %q", skill, routeCell)
			}
			rows = append(rows, fixtureRoute)
		}
		want := make([]fastRouteFixture, 0)
		for _, route := range fixture {
			if route.Skill != skill {
				continue
			}
			want = append(want, route)
		}
		if len(rows) != len(want) {
			t.Fatalf("%s route rows=%d fixture rows=%d", skill, len(rows), len(want))
		}
		for index := range rows {
			rows[index].Skill = skill
			if rows[index].Outcome != want[index].Outcome || rows[index].Surface != want[index].Surface || rows[index].Route != want[index].Route {
				t.Fatalf("%s route row %d=%#v fixture=%#v", skill, index, rows[index], want[index])
			}
		}
	}
}
