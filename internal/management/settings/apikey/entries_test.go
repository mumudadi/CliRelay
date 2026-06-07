package apikey

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func setupAPIKeySettingsDB(t *testing.T) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "apikey-settings.db")
	if err := usage.InitDB(dbPath, config.RequestLogStorageConfig{}, time.UTC); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(usage.CloseDB)
}

func TestReplaceEntriesNormalizesAndValidates(t *testing.T) {
	setupAPIKeySettingsDB(t)

	service := NewService(
		func(values []string) ([]string, error) {
			out := make([]string, 0, len(values))
			for _, value := range values {
				if value == "kimi-B" {
					out = append(out, value)
				}
			}
			return out, nil
		},
		func(values []string) ([]string, error) { return values, nil },
		func(entry config.APIKeyEntry) error {
			if len(entry.AllowedChannelGroups) != 1 || entry.AllowedChannelGroups[0] != "pro" {
				return errors.New("unexpected channel groups")
			}
			return nil
		},
	)

	err := service.ReplaceEntries([]config.APIKeyEntry{{
		Key:                  " sk-test ",
		Name:                 " Test Key ",
		PermissionProfileID:  " standard ",
		AllowedChannels:      []string{"kimi-A", "kimi-B"},
		AllowedChannelGroups: []string{" Pro ", "pro"},
		SystemPrompt:         " keep me ",
	}})
	if err != nil {
		t.Fatalf("ReplaceEntries() error = %v", err)
	}

	got := usage.GetAPIKey("sk-test")
	if got == nil {
		t.Fatal("expected API key after ReplaceEntries")
	}
	if got.Name != "Test Key" || got.PermissionProfileID != "standard" {
		t.Fatalf("stored row = %#v", got)
	}
	if len(got.AllowedChannels) != 1 || got.AllowedChannels[0] != "kimi-B" {
		t.Fatalf("allowed channels = %#v", got.AllowedChannels)
	}
	if len(got.AllowedChannelGroups) != 1 || got.AllowedChannelGroups[0] != "pro" {
		t.Fatalf("allowed channel groups = %#v", got.AllowedChannelGroups)
	}
	if got.SystemPrompt != "keep me" {
		t.Fatalf("system prompt = %q, want %q", got.SystemPrompt, "keep me")
	}
}

func TestPatchEntryRejectsBlankAndDuplicateKeys(t *testing.T) {
	setupAPIKeySettingsDB(t)

	for _, row := range []usage.APIKeyRow{
		{Key: "sk-original", Name: "Original"},
		{Key: "sk-target", Name: "Target"},
	} {
		if err := usage.UpsertAPIKey(row); err != nil {
			t.Fatalf("UpsertAPIKey(%q): %v", row.Key, err)
		}
	}

	service := NewService(nil, nil, nil)

	err := service.PatchEntry(nil, stringPtr("sk-original"), APIKeyEntryPatch{Key: stringPtr("   ")})
	if !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("PatchEntry(blank key) error = %v, want %v", err, ErrKeyRequired)
	}

	err = service.PatchEntry(nil, stringPtr("sk-original"), APIKeyEntryPatch{Key: stringPtr("sk-target")})
	if !errors.Is(err, ErrAPIKeyExists) {
		t.Fatalf("PatchEntry(duplicate key) error = %v, want %v", err, ErrAPIKeyExists)
	}
}

func TestDeleteEntryByIndexReturnsDeletedKey(t *testing.T) {
	setupAPIKeySettingsDB(t)

	if err := usage.UpsertAPIKey(usage.APIKeyRow{Key: "sk-delete-me", Name: "Delete Me"}); err != nil {
		t.Fatalf("UpsertAPIKey: %v", err)
	}

	service := NewService(nil, nil, nil)
	index := 0
	deletedKey, err := service.DeleteEntry("", &index)
	if err != nil {
		t.Fatalf("DeleteEntry() error = %v", err)
	}
	if deletedKey != "sk-delete-me" {
		t.Fatalf("deleted key = %q, want %q", deletedKey, "sk-delete-me")
	}
	if got := usage.GetAPIKey("sk-delete-me"); got != nil {
		t.Fatalf("expected key to be deleted, got %#v", got)
	}
}

func stringPtr(value string) *string {
	return &value
}
