//go:build darwin || linux

package approvalbroker

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
)

// The fake UI exists only in this test executable. Production has no flag,
// environment variable, shell input, or callback for automatic approval.
func TestBrokerHelperProcess(t *testing.T) {
	index := -1
	for i, argument := range os.Args {
		if argument == "--" {
			index = i
			break
		}
	}
	if index < 0 || index+1 >= len(os.Args) {
		return
	}
	mode := os.Args[index+1]
	if os.Getenv("OW_SYNTHETIC_SECRET") != "" {
		os.Exit(10)
	}
	if mode == "no-connect" {
		os.Exit(1)
	}
	if mode == "approved" || mode == "rejected" || mode == "hang" {
		err := runHelper(context.Background(), os.Stdin, func(ctx context.Context, request approval.Request) (bool, error) {
			if mode == "hang" {
				<-ctx.Done()
				return false, ctx.Err()
			}
			return mode == "approved", nil
		})
		if err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	var bootstrap bootstrapMessage
	if readMessage(os.Stdin, &bootstrap) != nil || !validEndpoint(bootstrap.Endpoint) {
		os.Exit(3)
	}
	if index+2 < len(os.Args) {
		if os.WriteFile(os.Args[index+2], []byte(bootstrap.Endpoint), 0600) != nil {
			os.Exit(4)
		}
	}
	connection, err := net.Dial("unix", bootstrap.Endpoint)
	if err != nil {
		os.Exit(5)
	}
	challenge := bootstrap.Challenge
	if mode == "wrong-challenge" {
		challenge = strings.Repeat("0", 64)
	}
	if writeMessage(connection, helloMessage{Protocol: protocolID, Challenge: challenge}) != nil {
		os.Exit(6)
	}
	var prompt promptMessage
	if readMessage(connection, &prompt) != nil {
		os.Exit(7)
	}
	document, _ := requestDocument(prompt.Request)
	decision := "approved"
	if mode == "wrong-document" {
		document = strings.Repeat("0", 64)
	}
	if mode == "automatic" {
		decision = "automatic"
	}
	if writeMessage(connection, decisionMessage{Challenge: bootstrap.Challenge, Document: document, Decision: decision}) != nil {
		os.Exit(8)
	}
	_ = connection.Close()
	if mode == "crash-after-decision" {
		os.Exit(9)
	}
	os.Exit(0)
}

func helperFactory(mode, recordPath string) commandFactory {
	return func(ctx context.Context, executable string) *exec.Cmd {
		arguments := []string{"-test.run=^TestBrokerHelperProcess$", "--", mode}
		if recordPath != "" {
			arguments = append(arguments, recordPath)
		}
		return exec.CommandContext(ctx, executable, arguments...)
	}
}

func TestProtectedProcessExchangeAndCleanup(t *testing.T) {
	t.Setenv("OW_SYNTHETIC_SECRET", "synthetic-not-a-real-secret")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	request := testRequest(t, testSnapshot(t))
	for _, mode := range []string{"approved", "rejected", "wrong-challenge", "wrong-document", "automatic", "crash-after-decision", "no-connect"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			recordPath := filepath.Join(t.TempDir(), "endpoint")
			err := exchangeWithCommand(ctx, executable, request, helperFactory(mode, recordPath))
			if mode == "approved" && err != nil {
				t.Fatal(err)
			}
			if mode != "approved" && err == nil {
				t.Fatalf("%s decision was approved", mode)
			}
			if mode == "rejected" && !errors.Is(err, approval.ErrNotApproved) {
				t.Fatalf("rejection = %v", err)
			}
			if ctx.Err() != nil {
				t.Fatalf("exchange failed to finish/reap helper promptly: %v", err)
			}
			if endpoint, readErr := os.ReadFile(recordPath); readErr == nil {
				if _, statErr := os.Lstat(filepath.Dir(string(endpoint))); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("private IPC directory survived: %v", statErr)
				}
			}
		})
	}
}

func TestProcessCancellationLeavesNoApproval(t *testing.T) {
	executable, _ := os.Executable()
	service, broker, store, snapshot := testService(t, func(ctx context.Context, request approval.Request) error {
		return exchangeWithCommand(ctx, executable, request, helperFactory("hang", ""))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := service.Confirm(ctx, snapshot.OperationID, 1, snapshot.SnapshotDigest); err == nil {
		t.Fatal("cancelled dialog was approved")
	}
	if time.Since(started) > 3*time.Second || broker.pending != nil {
		t.Fatal("cancelled process was not reaped or left approval evidence")
	}
	state, err := store.GetOperationState(context.Background(), snapshot.OperationID, 1)
	if err != nil || state.Status == "approved" || state.Status == "executing" {
		t.Fatalf("cancelled UI granted execution: %+v, %v", state, err)
	}
}

func TestProcessDecisionFlowsThroughSQLiteVerifier(t *testing.T) {
	executable, _ := os.Executable()
	service, _, store, snapshot := testService(t, func(ctx context.Context, request approval.Request) error {
		return exchangeWithCommand(ctx, executable, request, helperFactory("approved", ""))
	})
	result, err := service.Confirm(context.Background(), snapshot.OperationID, 1, snapshot.SnapshotDigest)
	if err != nil || result.ApprovalID == "" {
		t.Fatalf("protected process approval: %+v, %v", result, err)
	}
	state, err := store.GetOperationState(context.Background(), snapshot.OperationID, 1)
	if err != nil || state.Status != "approved" {
		t.Fatalf("decision was not persisted: %+v, %v", state, err)
	}
}

func TestHelperClosesDialogWhenOwnerDisconnects(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "ow-approval-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(directory)
	endpoint := filepath.Join(directory, "broker.sock")
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(endpoint, 0600); err != nil {
		t.Fatal(err)
	}
	challenge, _ := randomID()
	var bootstrap bytes.Buffer
	if err := writeMessage(&bootstrap, bootstrapMessage{Endpoint: endpoint, Challenge: challenge}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stopListener := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stopListener()
	entered, finished := make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- runHelper(ctx, &bootstrap, func(ctx context.Context, request approval.Request) (bool, error) {
			close(entered)
			<-ctx.Done()
			return false, ctx.Err()
		})
	}()
	connection, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var hello helloMessage
	if err := readMessage(connection, &hello); err != nil {
		t.Fatal(err)
	}
	if err := writeMessage(connection, promptMessage{Challenge: challenge, Request: testRequest(t, testSnapshot(t))}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("helper did not enter the confirmation UI")
	}
	_ = connection.Close()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("disconnected Runtime received approval")
		}
	case <-ctx.Done():
		t.Fatal("dialog outlived its Runtime connection")
	}
}
