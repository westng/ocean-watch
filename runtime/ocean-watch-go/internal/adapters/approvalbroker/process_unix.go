//go:build darwin || linux

package approvalbroker

import (
	"bytes"
	"context"
	"crypto/subtle"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
)

type commandFactory func(context.Context, string) *exec.Cmd

func exchangeProcess(ctx context.Context, executable string, request approval.Request) error {
	return exchangeWithCommand(ctx, executable, request, func(ctx context.Context, executable string) *exec.Cmd {
		return exec.CommandContext(ctx, executable, "internal-approval-broker")
	})
}

// The private Unix socket is the only listener. The child receives its random
// challenge through an inherited anonymous pipe, never argv/environment. A
// socket path or same-user connection alone cannot authenticate a decision.
func exchangeWithCommand(ctx context.Context, executable string, request approval.Request, factory commandFactory) error {
	document, err := requestDocument(request)
	if err != nil {
		return err
	}
	// /tmp keeps the path below Unix-domain socket length limits on macOS.
	directory, err := os.MkdirTemp("/tmp", "ow-approval-")
	if err != nil {
		return ErrUnavailable
	}
	defer os.Remove(directory)
	endpoint := filepath.Join(directory, "broker.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: endpoint, Net: "unix"})
	if err != nil {
		return ErrUnavailable
	}
	defer listener.Close()
	if err := os.Chmod(endpoint, 0600); err != nil {
		return ErrUnavailable
	}
	handshakeDeadline := time.Now().Add(5 * time.Second)
	if request.ExpiresAt.Before(handshakeDeadline) {
		handshakeDeadline = request.ExpiresAt
	}
	if err := listener.SetDeadline(handshakeDeadline); err != nil {
		return ErrUnavailable
	}
	stopListener := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stopListener()
	challenge, err := randomID()
	if err != nil {
		return ErrUnavailable
	}
	var bootstrap bytes.Buffer
	if err := writeMessage(&bootstrap, bootstrapMessage{Endpoint: endpoint, Challenge: challenge}); err != nil {
		return err
	}
	childContext, cancel := context.WithCancel(ctx)
	defer cancel()
	command := factory(childContext, executable)
	command.Stdin = &bootstrap
	command.Stdout, command.Stderr = io.Discard, io.Discard
	command.Env = dialogEnvironment()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 2 * time.Second
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
	if err := command.Start(); err != nil {
		return ErrUnavailable
	}
	processDone := make(chan error, 1)
	go func() {
		err := command.Wait()
		// A child which exits before connecting must not leave Accept blocked.
		_ = listener.Close()
		processDone <- err
	}()
	defer func() {
		cancel()
		<-processDone
	}()
	connection, err := listener.Accept()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	defer connection.Close()
	stopConnection := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopConnection()
	if err := connection.SetDeadline(handshakeDeadline); err != nil {
		return ErrUnavailable
	}
	var hello helloMessage
	if err := readMessage(connection, &hello); err != nil || hello.Protocol != protocolID || !sameChallenge(hello.Challenge, challenge) {
		return approval.ErrUntrustedEvidence
	}
	if err := connection.SetDeadline(request.ExpiresAt); err != nil {
		return ErrUnavailable
	}
	if err := writeMessage(connection, promptMessage{Challenge: challenge, Request: request}); err != nil {
		return ErrUnavailable
	}
	var decision decisionMessage
	if err := readMessage(connection, &decision); err != nil {
		return ErrUnavailable
	}
	if !sameChallenge(decision.Challenge, challenge) || decision.Document != document {
		return approval.ErrUntrustedEvidence
	}
	// Require the helper to finish normally, as well as its bound decision.
	select {
	case err := <-processDone:
		processDone <- err // leave the single completion for deferred cleanup
		if err != nil {
			return ErrUnavailable
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if decision.Decision != "approved" {
		return approval.ErrNotApproved
	}
	return nil
}

// RunHelper is an internal executable entry, not a capability/tool. It reads
// only its inherited bootstrap pipe and has no configuration/credential ports.
func RunHelper(ctx context.Context, input io.Reader) error {
	return runHelper(ctx, input, nativeDialog)
}

func runHelper(ctx context.Context, input io.Reader, dialog func(context.Context, approval.Request) (bool, error)) error {
	if ctx == nil || input == nil || dialog == nil {
		return ErrUnavailable
	}
	// In production input is an inherited pipe. Even an incorrectly launched
	// internal helper must not wait indefinitely for its bootstrap document.
	stopBootstrap := func() bool { return true }
	if closer, ok := input.(io.Closer); ok {
		timer := time.AfterFunc(5*time.Second, func() { _ = closer.Close() })
		stopBootstrap = timer.Stop
	}
	defer stopBootstrap()
	var bootstrap bootstrapMessage
	if err := readMessage(input, &bootstrap); err != nil || !validEndpoint(bootstrap.Endpoint) || len(bootstrap.Challenge) != 64 {
		return ErrUnavailable
	}
	stopBootstrap()
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", bootstrap.Endpoint)
	if err != nil {
		return ErrUnavailable
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	// The initial exchange is bounded independently of the approval lifetime.
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return ErrUnavailable
	}
	if err := writeMessage(connection, helloMessage{Protocol: protocolID, Challenge: bootstrap.Challenge}); err != nil {
		return ErrUnavailable
	}
	var prompt promptMessage
	if err := readMessage(connection, &prompt); err != nil || !sameChallenge(prompt.Challenge, bootstrap.Challenge) {
		return approval.ErrUntrustedEvidence
	}
	document, err := requestDocument(prompt.Request)
	if err != nil {
		return err
	}
	now := time.Now()
	if !now.Before(prompt.Request.ExpiresAt) || prompt.Request.ExpiresAt.After(now.Add(5*time.Minute)) {
		return ErrUnavailable
	}
	bounded, cancel := context.WithDeadline(ctx, prompt.Request.ExpiresAt)
	defer cancel()
	if err := connection.SetDeadline(prompt.Request.ExpiresAt); err != nil {
		return ErrUnavailable
	}
	// No further request data is legal. Losing the owning Runtime must close
	// the UI even if that process could not run its normal cancellation path.
	ownerDone := make(chan struct{})
	go func() {
		var unexpected [1]byte
		_, _ = connection.Read(unexpected[:])
		cancel()
		close(ownerDone)
	}()
	defer func() {
		_ = connection.Close()
		<-ownerDone
	}()
	approved, err := dialog(bounded, prompt.Request)
	if err != nil || bounded.Err() != nil {
		return ErrUnavailable
	}
	decision := "rejected"
	if approved {
		decision = "approved"
	}
	return writeMessage(connection, decisionMessage{Challenge: bootstrap.Challenge, Document: document, Decision: decision})
}

func sameChallenge(a, b string) bool {
	return len(a) == 64 && len(b) == 64 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func validEndpoint(endpoint string) bool {
	parent := filepath.Dir(endpoint)
	if filepath.Dir(parent) != "/tmp" || !strings.HasPrefix(filepath.Base(parent), "ow-approval-") || filepath.Base(endpoint) != "broker.sock" {
		return false
	}
	directory, err := os.Lstat(parent)
	if err != nil || !directory.IsDir() || directory.Mode().Perm() != 0700 {
		return false
	}
	socket, err := os.Lstat(endpoint)
	return err == nil && socket.Mode()&os.ModeSocket != 0 && socket.Mode().Perm() == 0600
}

func dialogEnvironment() []string {
	result := []string{"PATH=/usr/bin:/bin"}
	for _, name := range []string{"HOME", "USER", "LANG", "LC_ALL", "DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"} {
		if value, ok := os.LookupEnv(name); ok {
			result = append(result, name+"="+value)
		}
	}
	return result
}
