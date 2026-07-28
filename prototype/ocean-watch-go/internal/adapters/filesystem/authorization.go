package filesystem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

type AuthorizationStore struct {
	Root string
}

const authorizationStateSchemaVersion = 2

func (store AuthorizationStore) ReadChannel(ctx context.Context, channel string) (domain.AuthorizationState, error) {
	select {
	case <-ctx.Done():
		return domain.AuthorizationState{}, ctx.Err()
	default:
	}
	if channel != "marketing" && channel != "qianchuan" {
		return domain.AuthorizationState{}, fmt.Errorf("unsupported authorization channel: %s", channel)
	}
	channelState, err := store.LoadChannel(ctx, channel)
	if err != nil {
		return domain.AuthorizationState{}, err
	}
	return sanitizedAuthorizationState(channelState)
}

func (store AuthorizationStore) LoadChannel(ctx context.Context, channel string) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if channel != "marketing" && channel != "qianchuan" {
		return nil, fmt.Errorf("unsupported authorization channel: %s", channel)
	}
	currentPath := filepath.Join(store.Root, "channels", channel, "current.json")
	channelState, err := store.readCurrent(currentPath, channel)
	if errors.Is(err, os.ErrNotExist) {
		channelState, err = store.readLegacy(channel)
	}
	if err != nil {
		return nil, err
	}
	if channelState == nil {
		channelState = map[string]any{}
	}
	return channelState, nil
}

func (store AuthorizationStore) CommitChannel(ctx context.Context, channel string, state map[string]any) error {
	if channel != "marketing" && channel != "qianchuan" {
		return fmt.Errorf("unsupported authorization channel: %s", channel)
	}
	lock, err := AcquireLock(ctx, filepath.Join(store.Root, "authorizations.json."+channel+".lock"), 0)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	return store.commitChannelUnlocked(ctx, channel, state)
}

func (store AuthorizationStore) UpdateChannel(
	ctx context.Context,
	channel string,
	update func(map[string]any) error,
) error {
	if channel != "marketing" && channel != "qianchuan" {
		return fmt.Errorf("unsupported authorization channel: %s", channel)
	}
	if update == nil {
		return errors.New("authorization update callback is required")
	}
	lock, err := AcquireLock(ctx, filepath.Join(store.Root, "authorizations.json."+channel+".lock"), 0)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	state, err := store.LoadChannel(ctx, channel)
	if err != nil {
		return err
	}
	if err := update(state); err != nil {
		return err
	}
	return store.commitChannelUnlocked(ctx, channel, state)
}

func (store AuthorizationStore) commitChannelUnlocked(ctx context.Context, channel string, state map[string]any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	generation, err := positiveGeneration(state["generation"])
	if err != nil {
		return fmt.Errorf("credential generation conflict: %w", err)
	}
	root := filepath.Join(store.Root, "channels", channel)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create authorization state directory: %w", err)
	}
	_ = os.Chmod(filepath.Dir(root), 0o700)
	_ = os.Chmod(root, 0o700)

	for {
		manifestPath := filepath.Join(root, fmt.Sprintf("manifest-%d.json", generation))
		existing, readErr := readJSONObject(manifestPath)
		if errors.Is(readErr, os.ErrNotExist) {
			break
		}
		if readErr != nil {
			return readErr
		}
		if authorizationValuesEqual(existing, state) {
			break
		}
		generation++
		state["generation"] = generation
	}

	manifestPath := filepath.Join(root, fmt.Sprintf("manifest-%d.json", generation))
	if _, statErr := os.Stat(manifestPath); errors.Is(statErr, os.ErrNotExist) {
		if err := AtomicWritePrivateJSON(manifestPath, state); err != nil {
			return fmt.Errorf("write authorization manifest: %w", err)
		}
	} else if statErr != nil {
		return fmt.Errorf("stat authorization manifest: %w", statErr)
	}
	digest, err := authorizationManifestChecksum(state)
	if err != nil {
		return err
	}
	written, err := readJSONObject(manifestPath)
	if err != nil {
		return err
	}
	writtenDigest, err := authorizationManifestChecksum(written)
	if err != nil {
		return err
	}
	if writtenDigest != digest {
		return errors.New("authorization state corrupt: manifest read-back verification failed")
	}
	current := map[string]any{
		"schema_version": authorizationStateSchemaVersion,
		"generation":     generation,
		"sha256":         digest,
	}
	if err := AtomicWritePrivateJSON(filepath.Join(root, "current.json"), current); err != nil {
		return fmt.Errorf("activate authorization manifest: %w", err)
	}
	return nil
}

func (store AuthorizationStore) readCurrent(path, channel string) (map[string]any, error) {
	current, err := readJSONObject(path)
	if err != nil {
		return nil, err
	}
	generation, err := positiveGeneration(current["generation"])
	if err != nil {
		return nil, fmt.Errorf("authorization state incomplete for %s: %w", channel, err)
	}
	manifestPath := filepath.Join(store.Root, "channels", channel, fmt.Sprintf("manifest-%d.json", generation))
	manifest, err := readJSONObject(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("authorization state incomplete for %s: missing current manifest", channel)
		}
		return nil, err
	}
	digest, err := authorizationManifestChecksum(manifest)
	if err != nil {
		return nil, err
	}
	if current["sha256"] != digest {
		return nil, fmt.Errorf("authorization state corrupt for %s: manifest checksum mismatch", channel)
	}
	return manifest, nil
}

func (store AuthorizationStore) readLegacy(channel string) (map[string]any, error) {
	path := filepath.Join(store.Root, "authorizations.json")
	state, err := readJSONObject(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	version, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(state["schema_version"])))
	if err != nil || version != authorizationStateSchemaVersion {
		return nil, errors.New("unsupported authorization state schema")
	}
	channels, _ := state["channels"].(map[string]any)
	if channels == nil {
		return map[string]any{}, nil
	}
	result, _ := channels[channel].(map[string]any)
	if result == nil {
		return map[string]any{}, nil
	}
	return result, nil
}

func readJSONObject(path string) (map[string]any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode authorization state: %w", err)
	}
	if value == nil {
		return nil, errors.New("authorization state must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, errors.New("authorization state contains multiple JSON values")
	} else if !errors.Is(err, os.ErrClosed) && err.Error() != "EOF" {
		return nil, fmt.Errorf("decode authorization state: %w", err)
	}
	return value, nil
}

func authorizationManifestChecksum(value map[string]any) (string, error) {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", fmt.Errorf("encode authorization manifest: %w", err)
	}
	payload := bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func authorizationValuesEqual(left, right map[string]any) bool {
	leftPayload, leftErr := authorizationCanonicalJSON(left)
	rightPayload, rightErr := authorizationCanonicalJSON(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftPayload, rightPayload)
}

func authorizationCanonicalJSON(value map[string]any) ([]byte, error) {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func positiveGeneration(value any) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
	if err != nil || parsed < 1 {
		return 0, errors.New("generation must be positive")
	}
	return parsed, nil
}

func sanitizedAuthorizationState(value map[string]any) (domain.AuthorizationState, error) {
	authorizations, _ := value["authorizations"].(map[string]any)
	if authorizations == nil {
		authorizations = map[string]any{}
	}
	accountIndex, _ := value["account_index"].(map[string]any)
	if accountIndex == nil {
		accountIndex = map[string]any{}
	}
	index, _ := value["advertiser_index"].(map[string]any)
	if index == nil {
		index = map[string]any{}
	}
	ids := make([]string, 0, len(index))
	for advertiserID := range index {
		if !decimalIdentifier(advertiserID) {
			return domain.AuthorizationState{}, fmt.Errorf("invalid advertiser ID in authorization state: %s", advertiserID)
		}
		ids = append(ids, advertiserID)
	}
	sort.Slice(ids, func(left, right int) bool {
		if len(ids[left]) != len(ids[right]) {
			return len(ids[left]) < len(ids[right])
		}
		return ids[left] < ids[right]
	})
	generation := 0
	if value["generation"] != nil {
		parsed, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value["generation"])))
		if err != nil || parsed < 0 {
			return domain.AuthorizationState{}, errors.New("invalid authorization generation")
		}
		generation = parsed
	}
	authorizationIDs := make([]string, 0, len(authorizations))
	for authorizationID := range authorizations {
		authorizationIDs = append(authorizationIDs, authorizationID)
	}
	sort.Strings(authorizationIDs)
	summaries := make([]domain.AuthorizationSummary, 0, len(authorizationIDs))
	pendingCount := 0
	partialCount := 0
	for _, authorizationID := range authorizationIDs {
		metadata, ok := authorizations[authorizationID].(map[string]any)
		if !ok {
			return domain.AuthorizationState{}, fmt.Errorf("invalid authorization metadata: %s", authorizationID)
		}
		revision := 1
		if metadata["token_revision"] != nil {
			parsed, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(metadata["token_revision"])))
			if err != nil || parsed < 1 {
				return domain.AuthorizationState{}, fmt.Errorf("invalid token revision for authorization: %s", authorizationID)
			}
			revision = parsed
		}
		accountIDs, err := authorizationAccountIDs(metadata["authorized_accounts"])
		if err != nil {
			return domain.AuthorizationState{}, fmt.Errorf("authorization %s: %w", authorizationID, err)
		}
		advertiserIDs, err := authorizationIDList(metadata["advertiser_ids"], "advertiser")
		if err != nil {
			return domain.AuthorizationState{}, fmt.Errorf("authorization %s: %w", authorizationID, err)
		}
		pending, _ := metadata["pending_account_sync"].(bool)
		issues, _ := metadata["account_discovery_issues"].([]any)
		if pending {
			pendingCount++
		}
		if len(issues) != 0 {
			partialCount++
		}
		summaries = append(summaries, domain.AuthorizationSummary{
			AuthorizationID: authorizationID, TokenRevision: revision,
			AccountIDs: accountIDs, AdvertiserIDs: advertiserIDs,
			PendingAccountSync: pending, AccountDiscoveryIssues: append([]any(nil), issues...),
			AccountDiscoveryComplete: len(issues) == 0,
		})
	}
	return domain.AuthorizationState{
		AuthorizationCount: len(authorizations), AuthorizedAccountCount: len(accountIndex),
		AdvertiserIDs: ids, Generation: generation,
		PendingAccountSyncCount: pendingCount, PartialAccountDiscoveryCount: partialCount,
		Authorizations: summaries,
	}, nil
}

func authorizationAccountIDs(value any) ([]string, error) {
	rows, _ := value.([]any)
	result := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("authorized account must be an object")
		}
		if row["account_id"] == nil {
			continue
		}
		accountID := strings.TrimSpace(fmt.Sprint(row["account_id"]))
		if !decimalIdentifier(accountID) {
			return nil, fmt.Errorf("invalid account ID: %s", accountID)
		}
		if !seen[accountID] {
			seen[accountID] = true
			result = append(result, accountID)
		}
	}
	return result, nil
}

func authorizationIDList(value any, kind string) ([]string, error) {
	values, _ := value.([]any)
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, raw := range values {
		identifier := strings.TrimSpace(fmt.Sprint(raw))
		if !decimalIdentifier(identifier) {
			return nil, fmt.Errorf("invalid %s ID: %s", kind, identifier)
		}
		if !seen[identifier] {
			seen[identifier] = true
			result = append(result, identifier)
		}
	}
	return result, nil
}

func decimalIdentifier(value string) bool {
	if value == "0" {
		return true
	}
	if value == "" || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
