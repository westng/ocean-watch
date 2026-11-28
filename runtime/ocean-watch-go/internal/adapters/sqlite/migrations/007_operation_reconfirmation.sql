ALTER TABLE operation_states ADD COLUMN confirmation_round INTEGER NOT NULL DEFAULT 0 CHECK (confirmation_round >= 0);

DROP TRIGGER operation_state_transition;
CREATE TRIGGER operation_state_transition
BEFORE UPDATE OF status ON operation_states
WHEN NOT (
    OLD.status = NEW.status OR
    (OLD.status = 'draft' AND NEW.status IN ('preparing', 'blocked', 'cancelled')) OR
    (OLD.status = 'preparing' AND NEW.status IN ('preflight_ready', 'blocked', 'cancelled')) OR
    (OLD.status = 'preflight_ready' AND NEW.status IN ('awaiting_approval', 'blocked', 'cancelled', 'expired')) OR
    (OLD.status = 'awaiting_approval' AND NEW.status IN ('approved', 'blocked', 'cancelled', 'expired')) OR
    (OLD.status = 'approved' AND NEW.status IN ('awaiting_approval', 'executing', 'cancelled', 'expired')) OR
    (OLD.status = 'executing' AND NEW.status IN ('succeeded', 'failed', 'partially_succeeded', 'unknown')) OR
    (OLD.status IN ('unknown', 'partially_succeeded') AND NEW.status = 'reconciling') OR
    (OLD.status = 'reconciling' AND NEW.status IN ('succeeded', 'failed', 'partially_succeeded', 'unknown'))
)
BEGIN
    SELECT RAISE(ABORT, 'invalid operation state transition');
END;
