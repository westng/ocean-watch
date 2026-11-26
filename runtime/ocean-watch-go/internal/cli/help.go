package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
)

func WriteHelp(writer io.Writer, invocation Invocation) {
	if invocation.Command.Domain != "" {
		_, _ = fmt.Fprintf(writer, "Usage: ocean-watch %s %s [options]\n\n%s\n", invocation.Command.Domain, invocation.Command.Action, invocation.Command.Description)
		if invocation.Command.Name() == "templates create" {
			_, _ = fmt.Fprintln(writer, "\nAgent input: --input-json JSON | --input-file PATH (use - for stdin)\nMissing choices return status=needs_input; complete input returns status=preview.\nSave the reviewed preview: --submit --expected-revision REV --preview-digest DIGEST\nPut channel, source_template and marketing/product/live fields in the JSON object.\nWithout structured input, the direct-human CLI uses its interactive wizard.")
		}
		return
	}
	if invocation.Domain != "" {
		_, _ = fmt.Fprintf(writer, "Usage: ocean-watch %s <action> [options]\n\nActions:\n", invocation.Domain)
		for _, command := range contracts.Commands {
			if command.Domain == invocation.Domain {
				_, _ = fmt.Fprintf(writer, "  %-24s %s\n", command.Action, command.Description)
			}
		}
		return
	}
	domains := map[string]struct{}{}
	for _, command := range contracts.Commands {
		domains[command.Domain] = struct{}{}
	}
	names := make([]string, 0, len(domains))
	for domain := range domains {
		names = append(names, domain)
	}
	sort.Strings(names)
	_, _ = fmt.Fprintln(writer, "Usage: ocean-watch <domain> <action> [options]")
	_, _ = fmt.Fprintln(writer, "\nOcean Engine operations for Codex\n\nDomains:")
	for _, domain := range names {
		_, _ = fmt.Fprintf(writer, "  %s\n", domain)
	}
	_, _ = fmt.Fprintln(writer, "\nUse 'ocean-watch <domain> --help' to list actions.")
}
