package sqlite

import (
	"fmt"
	"regexp"

	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
)

var operationIDForLease = regexp.MustCompile(`^op_[A-Za-z0-9_-]{1,128}$`)
var ownerIDForLease = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func domainOperationID(value string) bool {
	return operationIDForLease.MatchString(value)
}

func validateOwnerID(value string) error {
	if !ownerIDForLease.MatchString(value) {
		return fmt.Errorf("%w: owner_id is invalid", domain.ErrInvalidOperation)
	}
	return nil
}
