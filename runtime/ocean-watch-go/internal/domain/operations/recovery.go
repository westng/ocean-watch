package operations

import "time"

// DispatchRecord contains only persisted dispatch facts and verified readback.
// An absent record means the immutable item was never dispatched.
type DispatchRecord struct {
	ItemID     string        `json:"item_id"`
	State      DispatchState `json:"dispatch_state"`
	ObjectID   string        `json:"object_id,omitempty"`
	Applied    bool          `json:"applied"`
	ObservedAt time.Time     `json:"observed_at,omitempty"`
}

type Readback struct {
	Lease          ExecutionLease
	ItemID         string
	ObjectID       string
	EvidenceDigest string
	Now            time.Time
}

func (r Readback) Validate() error {
	if err := r.Lease.Validate(); err != nil {
		return err
	}
	if !safeIDPattern.MatchString(r.ItemID) || !safeIDPattern.MatchString(r.ObjectID) || !digestPattern.MatchString(r.EvidenceDigest) || r.Now.IsZero() {
		return ErrInvalidDispatch
	}
	return nil
}
