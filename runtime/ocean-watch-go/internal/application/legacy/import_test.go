package legacy

import (
	"context"
	"errors"
	"strings"
	"testing"

	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/legacy"
)

type readerStub struct {
	payload []byte
	err     error
}

func (reader readerStub) ReadLegacyJournal(context.Context, string) ([]byte, error) {
	return reader.payload, reader.err
}

type repositoryStub struct {
	imports        int
	blockerQueries int
	queryErr       error
	blockers       []domain.Blocker
}

func (repo *repositoryStub) ImportLegacyMapping(_ context.Context, mapping domain.Mapping) (domain.Mapping, bool, error) {
	repo.imports++
	return mapping, true, nil
}
func (*repositoryStub) GetLegacyMapping(context.Context, string, string) (domain.Mapping, error) {
	return domain.Mapping{}, domain.ErrMappingNotFound
}
func (repo *repositoryStub) LegacyBlockers(context.Context, domain.Scope) ([]domain.Blocker, error) {
	repo.blockerQueries++
	return repo.blockers, repo.queryErr
}

func TestLegacyImporterRejectsUnsafeInputBeforeRepositoryWrite(t *testing.T) {
	valid := []byte(`{"fingerprint":"synthetic","jobs":{"job":{"status":"pending"}}}`)
	for _, tt := range []struct {
		name     string
		payload  []byte
		checksum string
		readErr  error
	}{
		{"duplicate", []byte(`{"jobs":{},"jobs":{}}`), "", nil},
		{"changed", valid, strings.Repeat("a", 64), nil},
		{"read-failure", valid, "", errors.New("synthetic read failure")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &repositoryStub{}
			checksum := tt.checksum
			if checksum == "" {
				checksum = domain.Digest(tt.payload)
			}
			importer := Importer{Source: readerStub{tt.payload, tt.readErr}, Mappings: repo}
			_, _, err := importer.Import(context.Background(), domain.Source{RootID: "synthetic", RecordID: "runs/test.json", Checksum: checksum})
			if err == nil || repo.imports != 0 {
				t.Fatalf("err=%v writes=%d", err, repo.imports)
			}
		})
	}
}

func TestLegacyCheckWritesFailsClosed(t *testing.T) {
	scope := domain.Scope{Channel: "marketing", AdvertiserID: "123"}
	repo := &repositoryStub{}
	if err := CheckWrites(context.Background(), repo, domain.Scope{}); !errors.Is(err, domain.ErrWriteBlocked) || repo.blockerQueries != 0 {
		t.Fatalf("unknown scope err=%v reads=%d", err, repo.blockerQueries)
	}
	if err := CheckWrites(context.Background(), repo, scope); err != nil {
		t.Fatal(err)
	}
	repo.queryErr = errors.New("synthetic unavailable repository")
	if err := CheckWrites(context.Background(), repo, scope); err == nil {
		t.Fatal("query failure allowed writes")
	}
	repo.queryErr = nil
	repo.blockers = []domain.Blocker{{Scope: domain.Scope{Channel: domain.Unknown, AdvertiserID: domain.Unknown}}}
	if err := CheckWrites(context.Background(), repo, scope); !errors.Is(err, domain.ErrWriteBlocked) {
		t.Fatal("root quarantine allowed writes")
	}
}
