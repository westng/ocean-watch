package approvalbroker

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
)

func platformAvailable() bool {
	info, err := os.Stat("/usr/bin/zenity")
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 &&
		(os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "")
}

func nativeDialog(ctx context.Context, request approval.Request) (bool, error) {
	text, err := request.Summary.ReviewText()
	if err != nil {
		return false, err
	}
	if !platformAvailable() {
		return false, ErrUnavailable
	}
	// A text view shows the complete document with literal text. Enter/Escape
	// cannot accept the default rejection; the user must select the approval.
	command := exec.CommandContext(ctx, "/usr/bin/zenity", "--text-info", "--title=Ocean Watch 投放审批", "--width=780", "--height=640", "--ok-label=批准此次操作", "--cancel-label=拒绝", "--checkbox=我已核对账户、对象和全部变更")
	command.Stdin = strings.NewReader(text)
	command.Env = dialogEnvironment()
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil {
		return false, approval.ErrNotApproved
	}
	return true, nil
}
