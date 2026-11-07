package bootstrap

import (
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/approvalbroker"
	application "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/state"
)

// NewOperations is called only for explicit confirmation work, not by doctor
// or ordinary read/preflight startup. The caller owns its state store. Provider
// selection and the trust root are fixed here, outside transport parameters.
func NewOperations(repository state.OperationRepository, snapshots state.Snapshots) (application.Service, error) {
	if repository == nil || snapshots == nil {
		return application.Service{}, approval.ErrVerifierUnavailable
	}
	broker, err := approvalbroker.New()
	if err != nil {
		return application.Service{}, err
	}
	verifier, err := approval.NewVerifier(broker)
	if err != nil {
		return application.Service{}, err
	}
	return application.Service{Repository: repository, Snapshots: snapshots,
		Provider: broker, Evidence: broker, Verifier: verifier}, nil
}
