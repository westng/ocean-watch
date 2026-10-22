package operations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func ApprovalScopeDigest(snapshot Snapshot) (string, error) {
	if err := snapshot.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		Contract       string `json:"contract"`
		OperationID    string `json:"operation_id"`
		Version        int    `json:"version"`
		Kind           string `json:"kind"`
		Channel        string `json:"channel"`
		SubjectID      string `json:"subject_id"`
		SnapshotDigest string `json:"snapshot_digest"`
	}{"approval-scope-v1", snapshot.OperationID, snapshot.Version, snapshot.Kind, snapshot.Channel, snapshot.SubjectID, snapshot.SnapshotDigest})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func MatchSnapshotItem(snapshot Snapshot, request DispatchIntentRequest) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	if snapshot.OperationID != request.OperationID || snapshot.Version != request.Version {
		return ErrInvalidDispatch
	}
	var payload struct {
		Items []struct {
			ItemID         string `json:"item_id"`
			GroupID        string `json:"group_id"`
			Action         string `json:"action"`
			TargetID       string `json:"target_id"`
			PayloadDigest  string `json:"payload_digest"`
			ExpectedDigest string `json:"expected_digest"`
		} `json:"items"`
	}
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		return ErrInvalidDispatch
	}
	matches := 0
	for _, item := range payload.Items {
		if item.ItemID != request.ItemID {
			continue
		}
		matches++
		if item.GroupID != request.GroupID || item.Action != request.Action || item.TargetID != request.TargetID || item.PayloadDigest != request.PayloadDigest || item.ExpectedDigest != request.ExpectedDigest {
			return ErrInvalidDispatch
		}
	}
	if matches != 1 {
		return ErrInvalidDispatch
	}
	return nil
}
