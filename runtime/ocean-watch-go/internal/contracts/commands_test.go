package contracts

import "testing"

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
		seen[command.Name()] = true
	}
	if len(Commands) != 74 {
		t.Fatalf("command count = %d, want 74", len(Commands))
	}
}
