package legacy

import (
	"context"
	"errors"

	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/legacy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/state"
)

type SourceReader interface {
	ReadLegacyJournal(context.Context, string) ([]byte, error)
}

type Importer struct {
	Source    SourceReader
	Mappings  state.LegacyMappings
	Contracts []domain.RecoveryContract
}

func (importer Importer) Import(ctx context.Context, source domain.Source) (domain.Mapping, bool, error) {
	if ctx == nil || importer.Source == nil || importer.Mappings == nil {
		return domain.Mapping{}, false, errors.New("legacy importer dependencies are required")
	}
	if err := ctx.Err(); err != nil {
		return domain.Mapping{}, false, err
	}
	if err := source.Validate(); err != nil {
		return domain.Mapping{}, false, err
	}
	payload, err := importer.Source.ReadLegacyJournal(ctx, source.RecordID)
	if err != nil {
		return domain.Mapping{}, false, err
	}
	mapping, err := domain.Decode(source, payload, importer.Contracts)
	if err != nil {
		return domain.Mapping{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return domain.Mapping{}, false, err
	}
	return importer.Mappings.ImportLegacyMapping(ctx, mapping)
}

func CheckWrites(ctx context.Context, mappings state.LegacyMappings, scope domain.Scope) error {
	if ctx == nil || mappings == nil || !scope.Known() {
		return domain.ErrWriteBlocked
	}
	blockers, err := mappings.LegacyBlockers(ctx, scope)
	if err != nil {
		return err
	}
	if len(blockers) != 0 {
		return domain.ErrWriteBlocked
	}
	return nil
}
