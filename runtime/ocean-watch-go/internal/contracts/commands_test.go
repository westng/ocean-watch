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
		if seen[command.Name()] {
			t.Fatalf("duplicate command: %s", command.Name())
		}
		seen[command.Name()] = true
	}
	if len(Commands) != 78 {
		t.Fatalf("command count = %d, want 78", len(Commands))
	}
}
