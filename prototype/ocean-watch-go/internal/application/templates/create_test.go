package templates

import (
	"context"
	"errors"
	"testing"
)

func TestCreateSessionCancellationIsWriteFree(t *testing.T) {
	store := &lifecycleStoreSpy{config: map[string]any{"future": "preserved"}, revision: "r1"}
	session, err := (Creator{Store: store}).Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Finish(context.Background(), map[string]any{"future": "changed"}, false); err != nil {
		t.Fatal(err)
	}
	if store.reads != 1 || store.casCalls != 0 {
		t.Fatalf("canceled create session wrote state: %#v", store)
	}
}

func TestCreateSessionCommitUsesCapturedRevision(t *testing.T) {
	store := &lifecycleStoreSpy{config: map[string]any{"future": "preserved"}, revision: "r1"}
	session, err := (Creator{Store: store}).Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	updated := map[string]any{"future": "changed"}
	if err := session.Finish(context.Background(), updated, true); err != nil {
		t.Fatal(err)
	}
	if store.casCalls != 1 || store.revision != "r1" || store.written["future"] != "changed" {
		t.Fatalf("create session did not use CAS: %#v", store)
	}
}

func TestCreateSessionPropagatesConflictWithoutSecondRead(t *testing.T) {
	store := &createConflictStore{config: map[string]any{"value": 1}}
	session, err := (Creator{Store: store}).Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = session.Finish(context.Background(), map[string]any{"value": 2}, true)
	if err == nil || err.Error() != "conflict" || store.reads != 1 || store.casCalls != 1 {
		t.Fatalf("unexpected conflict behavior: err=%v store=%#v", err, store)
	}
}

type createConflictStore struct {
	config   map[string]any
	reads    int
	casCalls int
}

func (store *createConflictStore) Read(context.Context) (map[string]any, error) {
	store.reads++
	return store.config, nil
}

func (store *createConflictStore) ReadWithRevision(context.Context) (map[string]any, string, error) {
	store.reads++
	return store.config, "r1", nil
}

func (store *createConflictStore) CompareAndSwap(context.Context, string, map[string]any) error {
	store.casCalls++
	return errors.New("conflict")
}
