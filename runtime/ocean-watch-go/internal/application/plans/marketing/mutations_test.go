package marketing

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
)

func TestMutationReconciliation(t *testing.T) {
	t.Run("out-of-range IDs stop before credentials", func(t *testing.T) {
		credentials := &mutationCredentialSpy{}
		command := fixtureMutationCommand(true)
		command.ObjectIDs = []string{"9223372036854775808"}
		_, err := (MutationExecutor{Guard: sharedplans.GuardedExecutor{
			Credentials: credentials, Locks: fixtureLock{},
		}}).Execute(context.Background(), command)
		if err == nil || credentials.calls != 0 {
			t.Fatalf("invalid SDK ID crossed credential boundary: err=%v calls=%d", err, credentials.calls)
		}
	})

	t.Run("dry-run needs no credentials or write dependencies", func(t *testing.T) {
		result, err := (MutationExecutor{}).Execute(context.Background(), fixtureMutationCommand(false))
		if err != nil {
			t.Fatal(err)
		}
		if result.Mode != "dry_run" || result.Submitted || result.Endpoint != "/v3.0/promotion/budget/update/" ||
			len(result.Rows) != 2 || result.Rows[0].Status != "ready" {
			t.Fatalf("dry-run contract changed: %#v", result)
		}
	})

	t.Run("partial official failure returns nonzero", func(t *testing.T) {
		fixture := &mutationFixture{
			writeResult: portmarketing.MutationWriteResult{
				RequestID: "mutation-request",
				RowErrors: map[string]string{"3002": "official row rejection"},
			},
			snapshots: []portmarketing.MutationSnapshot{
				{ObjectID: "3001", Value: "500"},
				{ObjectID: "3002", Value: "500"},
			},
		}
		result, err := fixtureMutationExecutor(fixture, fixtureLock{}).
			Execute(context.Background(), fixtureMutationCommand(true))
		if err != nil {
			t.Fatal(err)
		}
		if result.ExitCode != 1 || result.SuccessCount != 1 || result.FailureCount != 1 ||
			result.Rows[0].Status != "completed" || result.Rows[1].Status != "failed" ||
			result.Rows[1].OfficialError != "official row rejection" || fixture.writeCalls != 1 ||
			fixture.readCalls != 1 {
			t.Fatalf("partial mutation result changed: result=%#v fixture=%#v", result, fixture)
		}
	})

	t.Run("unknown write is reconciled without replay", func(t *testing.T) {
		fixture := &mutationFixture{
			writeErr: unknownError(),
			snapshots: []portmarketing.MutationSnapshot{
				{ObjectID: "3001", Value: "500.00"},
				{ObjectID: "3002", Value: "500"},
			},
		}
		result, err := fixtureMutationExecutor(fixture, fixtureLock{}).
			Execute(context.Background(), fixtureMutationCommand(true))
		if err != nil {
			t.Fatal(err)
		}
		if result.ExitCode != 0 || result.SuccessCount != 2 ||
			result.DispatchState != domainplans.DispatchUnknown ||
			result.Rows[0].Status != "reconciled" || result.Rows[1].Status != "reconciled" ||
			fixture.writeCalls != 1 || fixture.readCalls != 1 {
			t.Fatalf("unknown mutation was not reconciled: result=%#v fixture=%#v", result, fixture)
		}
	})

	t.Run("missing and mismatched readback cannot succeed", func(t *testing.T) {
		fixture := &mutationFixture{snapshots: []portmarketing.MutationSnapshot{
			{ObjectID: "3001", Value: "499.99"},
		}}
		result, err := fixtureMutationExecutor(fixture, fixtureLock{}).
			Execute(context.Background(), fixtureMutationCommand(true))
		if err != nil {
			t.Fatal(err)
		}
		if result.ExitCode != 1 || result.SuccessCount != 0 || result.FailureCount != 2 ||
			result.Rows[0].Error != "official readback does not match the requested target" ||
			result.Rows[1].Error != "official readback did not return this object" {
			t.Fatalf("unconfirmed mutation was reported successful: %#v", result)
		}
	})
}

func TestMutationReconciliationSerializesSameAdvertiser(t *testing.T) {
	locker := &serialMutationLock{}
	fixture := &mutationFixture{
		delay: 25 * time.Millisecond,
		snapshots: []portmarketing.MutationSnapshot{
			{ObjectID: "3001", Value: "500"},
			{ObjectID: "3002", Value: "500"},
		},
	}
	executor := fixtureMutationExecutor(fixture, locker)
	start := make(chan struct{})
	results := make(chan MutationResult, 2)
	errorsCh := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			result, err := executor.Execute(context.Background(), fixtureMutationCommand(true))
			results <- result
			errorsCh <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
		if result := <-results; result.ExitCode != 0 {
			t.Fatalf("serialized mutation failed: %#v", result)
		}
	}
	if fixture.maxActive != 1 || locker.maxActive != 1 || locker.acquires != 2 {
		t.Fatalf("same-advertiser writes overlapped: fixture=%#v lock=%#v", fixture, locker)
	}
}

func fixtureMutationCommand(submit bool) MutationCommand {
	return MutationCommand{
		AdvertiserID: "1001", Submit: submit,
		Kind:      portmarketing.MutationPromotionBudget,
		ObjectIDs: []string{"3001", "3002"}, Value: "500.00",
	}
}

func fixtureMutationExecutor(fixture *mutationFixture, locker sharedplans.AdvertiserLocker) MutationExecutor {
	return MutationExecutor{
		Guard: sharedplans.GuardedExecutor{
			Credentials: fixtureCredentials{}, Locks: locker,
		},
		Writer: fixture,
		Reader: fixture,
	}
}

type mutationFixture struct {
	mu          sync.Mutex
	writeResult portmarketing.MutationWriteResult
	writeErr    error
	snapshots   []portmarketing.MutationSnapshot
	readErr     error
	delay       time.Duration
	writeCalls  int
	readCalls   int
	active      int
	maxActive   int
}

func (fixture *mutationFixture) ApplyMutation(
	_ context.Context,
	_ portmarketing.MutationRequest,
) (portmarketing.MutationWriteResult, error) {
	fixture.mu.Lock()
	fixture.writeCalls++
	fixture.active++
	if fixture.active > fixture.maxActive {
		fixture.maxActive = fixture.active
	}
	fixture.mu.Unlock()
	if fixture.delay != 0 {
		time.Sleep(fixture.delay)
	}
	fixture.mu.Lock()
	fixture.active--
	fixture.mu.Unlock()
	return fixture.writeResult, fixture.writeErr
}

func (fixture *mutationFixture) ReadMutation(
	context.Context,
	portmarketing.MutationRequest,
) ([]portmarketing.MutationSnapshot, error) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.readCalls++
	return append([]portmarketing.MutationSnapshot(nil), fixture.snapshots...), fixture.readErr
}

type serialMutationLock struct {
	mu        sync.Mutex
	statsMu   sync.Mutex
	active    int
	maxActive int
	acquires  int
}

type mutationCredentialSpy struct {
	calls int
}

func (spy *mutationCredentialSpy) AccessToken(
	context.Context,
	domainplans.Channel,
	string,
	string,
) (sharedplans.CredentialLease, error) {
	spy.calls++
	return sharedplans.CredentialLease{}, errors.New("credentials must not be read")
}

func (locker *serialMutationLock) Acquire(
	ctx context.Context,
	scope domainplans.WriteScope,
) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scope.Channel != domainplans.ChannelMarketing ||
		scope.LockFamily != domainplans.LockPlanSettings || scope.AdvertiserID != "1001" {
		return nil, errors.New("unexpected mutation lock scope")
	}
	locker.mu.Lock()
	locker.statsMu.Lock()
	locker.acquires++
	locker.active++
	if locker.active > locker.maxActive {
		locker.maxActive = locker.active
	}
	locker.statsMu.Unlock()
	return func() error {
		locker.statsMu.Lock()
		locker.active--
		locker.statsMu.Unlock()
		locker.mu.Unlock()
		return nil
	}, nil
}
