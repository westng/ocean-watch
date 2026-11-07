package approvalbroker

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
)

const dialogScript = `use framework "AppKit"
use scripting additions
on run argv
set applicationObject to current application's NSApplication's sharedApplication()
applicationObject's setActivationPolicy:0
set alertObject to current application's NSAlert's alloc()'s init()
alertObject's setMessageText:"Ocean Watch 投放审批"
alertObject's setInformativeText:"请核对下方全部变更。只有点击批准按钮才会记录此次确认；后续执行仍须重新检查有效期和操作版本。"
alertObject's setAlertStyle:1
set rejectButton to alertObject's addButtonWithTitle:"拒绝"
set approveButton to alertObject's addButtonWithTitle:"批准此次操作"
rejectButton's setKeyEquivalent:(character id 13)
approveButton's setKeyEquivalent:""
set textFrame to current application's NSMakeRect(0, 0, 680, 400)
set textView to current application's NSTextView's alloc()'s initWithFrame:textFrame
textView's setString:(item 1 of argv)
textView's setEditable:false
textView's setVerticallyResizable:true
textView's setHorizontallyResizable:false
(textView's textContainer())'s setContainerSize:(current application's NSMakeSize(660, 1000000))
(textView's textContainer())'s setWidthTracksTextView:true
set scrollView to current application's NSScrollView's alloc()'s initWithFrame:textFrame
scrollView's setHasVerticalScroller:true
scrollView's setDocumentView:textView
alertObject's setAccessoryView:scrollView
applicationObject's activateIgnoringOtherApps:true
set responseValue to alertObject's runModal()
if responseValue as integer is 1001 then return "approved"
return "rejected"
end run`

func platformAvailable() bool {
	info, err := os.Stat("/usr/bin/osascript")
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

func nativeDialog(ctx context.Context, request approval.Request) (bool, error) {
	text, err := request.Summary.ReviewText()
	if err != nil {
		return false, err
	}
	if time.Until(request.ExpiresAt) <= 0 || time.Until(request.ExpiresAt) > 5*time.Minute {
		return false, ErrUnavailable
	}
	// Review text is an argument, never AppleScript source. The default and
	// cancel buttons both reject; only the explicit approval button succeeds.
	command := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", dialogScript, "--", text)
	command.Env = dialogEnvironment()
	command.WaitDelay = time.Second
	output, err := command.Output()
	if err != nil {
		return false, approval.ErrNotApproved
	}
	return strings.TrimSpace(string(output)) == "approved", nil
}
