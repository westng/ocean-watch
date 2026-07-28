package cli

import (
	"fmt"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/contracts"
)

const (
	localCommandRequestLimit    int64 = 0
	tokenCommandRequestLimit    int64 = 8
	accountReportRequestLimit   int64 = 4096
	paginatedReadRequestLimit   int64 = 4096
	creatorReadRequestLimit     int64 = 16384
	mutationCommandRequestLimit int64 = 65536
)

func commandRequestLimit(command contracts.Command) (int64, error) {
	switch command.Domain {
	case "setup", "runs", "templates", "qc-templates", "mcp":
		return localCommandRequestLimit, nil
	case "accounts":
		if command.Action == "report" {
			return accountReportRequestLimit, nil
		}
		return localCommandRequestLimit, nil
	case "auth":
		switch command.Action {
		case "authorize", "sync-accounts":
			return paginatedReadRequestLimit, nil
		case "refresh":
			return tokenCommandRequestLimit, nil
		case "set-app", "status", "migrate", "mappings":
			return localCommandRequestLimit, nil
		}
	case "materials", "qc-products", "reports", "qc-reports", "discover":
		return paginatedReadRequestLimit, nil
	case "qc-materials":
		if command.Action == "creator-videos" {
			return creatorReadRequestLimit, nil
		}
		return paginatedReadRequestLimit, nil
	case "qc-plans":
		switch command.Action {
		case "update-status", "update-budget", "update-roi":
			return mutationCommandRequestLimit, nil
		default:
			return paginatedReadRequestLimit, nil
		}
	case "plans":
		return mutationCommandRequestLimit, nil
	}
	return 0, fmt.Errorf("request budget profile is missing for command %s", command.Name())
}
