package templates

import "context"

type CreateSession struct {
	store    ConfigStore
	revision string
	config   map[string]any
}

type Creator struct {
	Store ConfigStore
}

func (creator Creator) Begin(ctx context.Context) (*CreateSession, error) {
	config, revision, err := creator.Store.ReadWithRevision(ctx)
	if err != nil {
		return nil, err
	}
	return &CreateSession{
		store:    creator.Store,
		revision: revision,
		config:   config,
	}, nil
}

func (session *CreateSession) Config() map[string]any {
	return session.config
}

func (session *CreateSession) Finish(ctx context.Context, updated map[string]any, confirmed bool) error {
	if !confirmed {
		return nil
	}
	return session.store.CompareAndSwap(ctx, session.revision, updated)
}
