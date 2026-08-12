package contracts

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestCommandsMatchFrozenManifest(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "..", "..", "contracts", "commands.yaml")
	file, err := os.Open(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	pattern := regexp.MustCompile(`^\s{2}command: (.+)$`)
	var manifestCommands []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		match := pattern.FindStringSubmatch(scanner.Text())
		if len(match) == 2 {
			manifestCommands = append(manifestCommands, match[1])
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	actual := Names()
	if len(actual) != 81 || len(manifestCommands) != len(actual) {
		t.Fatalf("command counts differ: go=%d manifest=%d", len(actual), len(manifestCommands))
	}
	for index := range actual {
		if actual[index] != manifestCommands[index] {
			t.Fatalf("command %d differs: go=%q manifest=%q", index, actual[index], manifestCommands[index])
		}
	}
}
