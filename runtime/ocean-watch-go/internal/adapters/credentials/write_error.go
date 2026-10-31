package credentials

// Retain the cause for local classification without presenting command output.
type writeError struct {
	cause  error
	reason string
}

func (err *writeError) Error() string                      { return "secure credential storage failed" }
func (err *writeError) Unwrap() error                      { return err.cause }
func (err *writeError) AuthorizationFailureReason() string { return err.reason }
