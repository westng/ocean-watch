//go:build !darwin && !linux

package approvalbroker

import (
	"context"
	"io"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
)

// Windows remains unavailable until its protected named-pipe transport and
// native UI have their own evidence. There is no TCP or terminal fallback.
func platformAvailable() bool { return false }

func exchangeProcess(context.Context, string, approval.Request) error { return ErrUnavailable }

func RunHelper(context.Context, io.Reader) error { return ErrUnavailable }
