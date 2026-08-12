package domain

import "fmt"

type Error struct {
	Code     string
	Message  string
	Details  map[string]any
	ExitCode int
}

func (e *Error) Error() string { return e.Message }

func NewError(code, message string, exitCode int, details map[string]any) *Error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{Code: code, Message: message, Details: details, ExitCode: exitCode}
}

func WrapError(code, message string, exitCode int, err error) *Error {
	return NewError(code, message, exitCode, map[string]any{"cause": fmt.Sprint(err)})
}
