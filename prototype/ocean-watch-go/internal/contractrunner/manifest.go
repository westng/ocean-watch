package contractrunner

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/contracts"
)

var commandLinePattern = regexp.MustCompile(`^\s{2}command: (.+)$`)

func ReadCommandManifest(path string) ([]string, string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read command manifest: %w", err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(payload)))
	var commands []string
	for scanner.Scan() {
		match := commandLinePattern.FindStringSubmatch(scanner.Text())
		if len(match) == 2 {
			commands = append(commands, match[1])
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, "", fmt.Errorf("scan command manifest: %w", err)
	}
	expected := contracts.Names()
	if len(commands) != len(expected) {
		return nil, "", fmt.Errorf("command manifest count is %d, Go contract count is %d", len(commands), len(expected))
	}
	for index := range expected {
		if commands[index] != expected[index] {
			return nil, "", fmt.Errorf("command manifest mismatch at %d: %q != %q", index, commands[index], expected[index])
		}
	}
	digest := sha256.Sum256(payload)
	return commands, hex.EncodeToString(digest[:]), nil
}

func BuiltinCases(commands []string) []CapturedCase {
	cases := []CapturedCase{
		{Category: "version", Spec: CaseSpec{ID: "version", Argv: []string{"--version"}}},
		{Category: "help", Spec: CaseSpec{ID: "help-global", Argv: []string{"--help"}}},
	}
	domains := map[string]struct{}{}
	for _, name := range commands {
		domain, action, ok := splitCommand(name)
		if !ok {
			panic("validated command manifest contains an invalid command")
		}
		domains[domain] = struct{}{}
		cases = append(cases, CapturedCase{
			Category: "help",
			Spec:     CaseSpec{ID: "help-" + domain + "-" + action, Argv: []string{domain, action, "--help"}},
		})
	}
	orderedDomains := make([]string, 0, len(domains))
	for domain := range domains {
		orderedDomains = append(orderedDomains, domain)
	}
	sort.Strings(orderedDomains)
	domainCases := make([]CapturedCase, 0, len(orderedDomains))
	for _, domain := range orderedDomains {
		domainCases = append(domainCases, CapturedCase{
			Category: "help",
			Spec:     CaseSpec{ID: "help-domain-" + domain, Argv: []string{domain, "--help"}},
		})
	}
	return append(cases[:2], append(domainCases, cases[2:]...)...)
}

func splitCommand(name string) (string, string, bool) {
	for index := 0; index < len(name); index++ {
		if name[index] == ' ' && index > 0 && index+1 < len(name) {
			return name[:index], name[index+1:], true
		}
	}
	return "", "", false
}
