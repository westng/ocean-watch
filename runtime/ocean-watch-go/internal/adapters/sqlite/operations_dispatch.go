package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
)

func (store *Store) BeginDispatch(ctx context.Context, request domain.DispatchIntentRequest) (domain.DispatchAttempt, error) {
	if err := request.Validate(); err != nil {
		return domain.DispatchAttempt{}, err
	}
	key, err := domain.DispatchKey(request)
	if err != nil {
		return domain.DispatchAttempt{}, err
	}
	tx, err := store.beginOperationTx(ctx)
	if err != nil {
		return domain.DispatchAttempt{}, err
	}
	defer tx.Rollback()
	if err := checkOperationLegacyBlockers(ctx, tx, request.OperationID); err != nil {
		return domain.DispatchAttempt{}, err
	}
	snapshot, err := currentOperationSnapshot(ctx, tx, request.OperationID, request.Version, request.Now)
	if err != nil {
		return domain.DispatchAttempt{}, err
	}
	if err := domain.MatchSnapshotItem(snapshot, request); err != nil {
		return domain.DispatchAttempt{}, err
	}
	if _, err := checkLease(ctx, tx, request); err != nil {
		return domain.DispatchAttempt{}, err
	}
	state, err := readOperationState(ctx, tx, request.OperationID, request.Version)
	if err != nil {
		return domain.DispatchAttempt{}, err
	}
	if state.Status != domain.StatusExecuting {
		return domain.DispatchAttempt{}, fmt.Errorf("%w: status is %s", domain.ErrOperationNotExecutable, state.Status)
	}
	var existingOperation, existingItem, existingGroup, existingAction, existingTarget, existingExpected, existingPayload, existingStatus string
	err = tx.QueryRowContext(ctx, `SELECT operation_id, item_id, group_id, action, target_id, expected_digest, payload_digest, status
		FROM operation_items WHERE operation_id=? AND operation_version=? AND dispatch_key=?`, request.OperationID, request.Version, key).Scan(&existingOperation, &existingItem, &existingGroup, &existingAction, &existingTarget, &existingExpected, &existingPayload, &existingStatus)
	if err == nil {
		if existingOperation != request.OperationID || existingItem != request.ItemID || existingGroup != request.GroupID || existingAction != request.Action || existingTarget != request.TargetID || existingExpected != request.ExpectedDigest || existingPayload != request.PayloadDigest {
			return domain.DispatchAttempt{}, domain.ErrInvalidDispatch
		}
		switch domain.ItemStatus(existingStatus) {
		case domain.ItemAcknowledged:
			return domain.DispatchAttempt{}, domain.ErrDispatchAcknowledged
		case domain.ItemUnknown:
			return domain.DispatchAttempt{}, domain.ErrDispatchUnknown
		case domain.ItemIntent:
			return domain.DispatchAttempt{}, domain.ErrDispatchInProgress
		case domain.ItemPending, domain.ItemNotSent:
		default:
			return domain.DispatchAttempt{}, domain.ErrInvalidDispatch
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domain.DispatchAttempt{}, err
	}
	if err := ensureDispatchItem(ctx, tx, request, key); err != nil {
		return domain.DispatchAttempt{}, err
	}
	var attemptNo int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt_no), 0) + 1 FROM dispatch_attempts
		WHERE operation_id=? AND operation_version=? AND item_id=?`, request.OperationID, request.Version, request.ItemID).Scan(&attemptNo); err != nil {
		return domain.DispatchAttempt{}, err
	}
	attempt := domain.DispatchAttempt{
		OperationID: request.OperationID, Version: request.Version, ItemID: request.ItemID,
		AttemptNo: attemptNo, DispatchKey: key, State: domain.DispatchIntent, StartedAt: request.Now.UTC(),
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO dispatch_attempts(operation_id, operation_version, item_id, attempt_no, dispatch_key, state, official_request_id, started_at)
		VALUES (?, ?, ?, ?, ?, 'intent', '', ?)`, attempt.OperationID, attempt.Version, attempt.ItemID, attempt.AttemptNo,
		attempt.DispatchKey, operationTime(attempt.StartedAt)); err != nil {
		return domain.DispatchAttempt{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE operation_items SET status='intent', updated_at=?
		WHERE operation_id=? AND operation_version=? AND item_id=?`, operationTime(request.Now), request.OperationID, request.Version, request.ItemID); err != nil {
		return domain.DispatchAttempt{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.DispatchAttempt{}, err
	}
	return attempt, nil
}

func ensureDispatchItem(ctx context.Context, tx *sql.Tx, request domain.DispatchIntentRequest, key string) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO operation_items(operation_id, operation_version, item_id, group_id, action, target_id,
		expected_digest, payload_digest, status, dispatch_key, failure_code, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, '', ?)`, request.OperationID, request.Version, request.ItemID,
		request.GroupID, request.Action, request.TargetID, request.ExpectedDigest, request.PayloadDigest, key, operationTime(request.Now))
	return err
}

func (store *Store) CompleteDispatch(ctx context.Context, outcome domain.DispatchOutcome) error {
	if err := outcome.Validate(); err != nil {
		return err
	}
	tx, err := store.beginOperationTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	leaseRequest := domain.DispatchIntentRequest{OperationID: outcome.OperationID, Version: outcome.Version, ItemID: outcome.ItemID, OwnerID: outcome.OwnerID, FencingEpoch: outcome.FencingEpoch, Now: outcome.Now}
	if _, err := checkLease(ctx, tx, leaseRequest); err != nil {
		return err
	}
	state, err := readOperationState(ctx, tx, outcome.OperationID, outcome.Version)
	if err != nil {
		return err
	}
	if state.Status != domain.StatusExecuting {
		return fmt.Errorf("%w: status is %s", domain.ErrOperationNotExecutable, state.Status)
	}
	var attemptNo int
	var attemptState string
	err = tx.QueryRowContext(ctx, `SELECT attempt_no, state FROM dispatch_attempts
		WHERE operation_id=? AND operation_version=? AND item_id=? AND dispatch_key=?
		ORDER BY attempt_no DESC LIMIT 1`, outcome.OperationID, outcome.Version, outcome.ItemID, outcome.DispatchKey).Scan(&attemptNo, &attemptState)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrInvalidDispatch
	}
	if err != nil {
		return err
	}
	if attemptState != string(domain.DispatchIntent) {
		if attemptState == string(domain.DispatchAcknowledged) {
			return domain.ErrDispatchAcknowledged
		}
		if attemptState == string(domain.DispatchUnknown) {
			return domain.ErrDispatchUnknown
		}
		return domain.ErrInvalidDispatch
	}
	finished := outcome.Now.UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE dispatch_attempts SET state=?, official_request_id=?, finished_at=?, response_digest=?
		WHERE operation_id=? AND operation_version=? AND item_id=? AND attempt_no=? AND state='intent'`,
		string(outcome.State), outcome.OfficialRequestID, operationTime(finished), nullableDigest(outcome.ResponseDigest),
		outcome.OperationID, outcome.Version, outcome.ItemID, attemptNo); err != nil {
		return err
	}
	itemStatus := mapDispatchItemStatus(outcome.State)
	if _, err := tx.ExecContext(ctx, `UPDATE operation_items SET status=?, updated_at=?
		WHERE operation_id=? AND operation_version=? AND item_id=? AND status='intent'`,
		string(itemStatus), operationTime(finished), outcome.OperationID, outcome.Version, outcome.ItemID); err != nil {
		return err
	}
	if outcome.State == domain.DispatchUnknown {
		if _, err := tx.ExecContext(ctx, `UPDATE operation_states SET status='unknown', updated_at=?
			WHERE operation_id=? AND operation_version=? AND status='executing'`, operationTime(finished), outcome.OperationID, outcome.Version); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullableDigest(value string) any {
	if value == "" {
		return nil
	}
	return value
}
