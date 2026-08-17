package cli

import (
	"fmt"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
)

const (
	localCommandRequestLimit    int64 = 0
	tokenCommandRequestLimit    int64 = 8
	accountReportRequestLimit   int64 = 4096
	paginatedReadRequestLimit   int64 = 4096
	creatorReadRequestLimit     int64 = 16384
	mutationCommandRequestLimit int64 = 65536
)

type requestBudgetProfile struct {
	limit     int64
	unbounded bool
}

func boundedRequestBudget(limit int64) requestBudgetProfile {
	return requestBudgetProfile{limit: limit}
}

func commandRequestBudgetProfile(command contracts.Command) (requestBudgetProfile, error) {
	switch command.Domain {
	case "setup", "runs", "templates", "qc-templates":
		return boundedRequestBudget(localCommandRequestLimit), nil
	case "accounts":
		if command.Action == "report" {
			return boundedRequestBudget(accountReportRequestLimit), nil
		}
		return boundedRequestBudget(localCommandRequestLimit), nil
	case "auth":
		switch command.Action {
		case "authorize", "sync-accounts":
			return boundedRequestBudget(paginatedReadRequestLimit), nil
		case "refresh":
			return boundedRequestBudget(tokenCommandRequestLimit), nil
		case "set-app", "status", "mappings":
			return boundedRequestBudget(localCommandRequestLimit), nil
		}
	case "materials", "qc-products", "reports", "qc-reports", "discover":
		return boundedRequestBudget(paginatedReadRequestLimit), nil
	case "qc-materials":
		if command.Action == "creator-videos" {
			return boundedRequestBudget(creatorReadRequestLimit), nil
		}
		return boundedRequestBudget(paginatedReadRequestLimit), nil
	case "qc-plans":
		switch command.Action {
		case "update-status", "update-budget", "update-roi":
			return boundedRequestBudget(mutationCommandRequestLimit), nil
		default:
			return boundedRequestBudget(paginatedReadRequestLimit), nil
		}
	case "plans":
		if command.Action == "batch-qianchuan-works" || command.Action == "remove-qianchuan-work" {
			return requestBudgetProfile{unbounded: true}, nil
		}
		return boundedRequestBudget(mutationCommandRequestLimit), nil
	}
	return requestBudgetProfile{}, fmt.Errorf("request budget profile is missing for command %s", command.Name())
}
