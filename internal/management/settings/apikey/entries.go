package apikey

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	internalrouting "github.com/router-for-me/CLIProxyAPI/v6/internal/routing"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

type APIKeyEntryPatch struct {
	Key                  *string   `json:"key"`
	Name                 *string   `json:"name"`
	PermissionProfileID  *string   `json:"permission-profile-id"`
	DailyLimit           *int      `json:"daily-limit"`
	TotalQuota           *int      `json:"total-quota"`
	SpendingLimit        *float64  `json:"spending-limit"`
	ConcurrencyLimit     *int      `json:"concurrency-limit"`
	RPMLimit             *int      `json:"rpm-limit"`
	TPMLimit             *int      `json:"tpm-limit"`
	AllowedModels        *[]string `json:"allowed-models"`
	AllowedChannels      *[]string `json:"allowed-channels"`
	AllowedChannelGroups *[]string `json:"allowed-channel-groups"`
	SystemPrompt         *string   `json:"system-prompt"`
	CreatedAt            *string   `json:"created-at"`
}

func (s *Service) APIKeyEntries() []config.APIKeyEntry {
	rows := usage.EffectiveAPIKeyRows(usage.ListAPIKeys())
	entries := make([]config.APIKeyEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, row.ToConfigEntry())
	}
	return entries
}

func (s *Service) ReplaceEntries(entries []config.APIKeyEntry) error {
	rows := make([]usage.APIKeyRow, 0, len(entries))
	for _, entry := range entries {
		normalized, err := s.normalizeEntry(entry)
		if err != nil {
			return err
		}
		rows = append(rows, usage.APIKeyRowFromConfig(normalized))
	}
	return usage.ReplaceAllAPIKeys(rows)
}

func (s *Service) PatchEntry(index *int, match *string, patch APIKeyEntryPatch) error {
	targetKey := ""
	if match != nil {
		targetKey = strings.TrimSpace(*match)
	}
	if targetKey == "" && index != nil {
		rows := usage.ListAPIKeys()
		if *index >= 0 && *index < len(rows) {
			targetKey = rows[*index].Key
		}
	}
	if targetKey == "" {
		return ErrItemNotFound
	}

	var entry usage.APIKeyRow
	if existing := usage.GetAPIKey(targetKey); existing != nil {
		entry = *existing
	} else {
		entry.Key = targetKey
	}

	if patch.Key != nil {
		trimmed := strings.TrimSpace(*patch.Key)
		if trimmed == "" {
			return ErrKeyRequired
		}
		if trimmed != targetKey {
			if existing := usage.GetAPIKey(trimmed); existing != nil {
				return ErrAPIKeyExists
			}
			if err := usage.DeleteAPIKey(targetKey); err != nil {
				return err
			}
		}
		entry.Key = trimmed
	}
	if patch.Name != nil {
		entry.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.PermissionProfileID != nil {
		entry.PermissionProfileID = strings.TrimSpace(*patch.PermissionProfileID)
	}
	if patch.DailyLimit != nil {
		entry.DailyLimit = *patch.DailyLimit
	}
	if patch.TotalQuota != nil {
		entry.TotalQuota = *patch.TotalQuota
	}
	if patch.SpendingLimit != nil {
		entry.SpendingLimit = *patch.SpendingLimit
	}
	if patch.ConcurrencyLimit != nil {
		entry.ConcurrencyLimit = *patch.ConcurrencyLimit
	}
	if patch.RPMLimit != nil {
		entry.RPMLimit = *patch.RPMLimit
	}
	if patch.TPMLimit != nil {
		entry.TPMLimit = *patch.TPMLimit
	}
	if patch.AllowedModels != nil {
		entry.AllowedModels = append([]string(nil), (*patch.AllowedModels)...)
	}
	if patch.AllowedChannels != nil {
		entry.AllowedChannels = append([]string(nil), (*patch.AllowedChannels)...)
	}
	if patch.AllowedChannelGroups != nil {
		entry.AllowedChannelGroups = append([]string(nil), (*patch.AllowedChannelGroups)...)
	}
	if patch.SystemPrompt != nil {
		entry.SystemPrompt = strings.TrimSpace(*patch.SystemPrompt)
	}
	if patch.CreatedAt != nil {
		entry.CreatedAt = strings.TrimSpace(*patch.CreatedAt)
	}

	normalized, err := s.normalizeEntry(entry.ToConfigEntry())
	if err != nil {
		return err
	}
	return usage.UpsertAPIKey(usage.APIKeyRowFromConfig(normalized))
}

func (s *Service) DeleteEntry(key string, index *int) (string, error) {
	if trimmed := strings.TrimSpace(key); trimmed != "" {
		return trimmed, usage.DeleteAPIKey(trimmed)
	}
	if index != nil {
		rows := usage.ListAPIKeys()
		if *index >= 0 && *index < len(rows) {
			keyValue := rows[*index].Key
			return keyValue, usage.DeleteAPIKey(keyValue)
		}
	}
	return "", ErrMissingValue
}

func (s *Service) normalizeEntry(entry config.APIKeyEntry) (config.APIKeyEntry, error) {
	entry.Key = strings.TrimSpace(entry.Key)
	entry.Name = strings.TrimSpace(entry.Name)
	entry.PermissionProfileID = strings.TrimSpace(entry.PermissionProfileID)
	entry.SystemPrompt = strings.TrimSpace(entry.SystemPrompt)
	entry.CreatedAt = strings.TrimSpace(entry.CreatedAt)
	entry.AllowedChannelGroups = uniqueChannelGroups(entry.AllowedChannelGroups)

	if s != nil && s.sanitizeChannels != nil {
		cleaned, err := s.sanitizeChannels(entry.AllowedChannels)
		if err != nil {
			return config.APIKeyEntry{}, fmt.Errorf("%w: %v", ErrInvalidEntry, err)
		}
		entry.AllowedChannels = cleaned
	}
	if s != nil && s.sanitizeChannelGroups != nil {
		cleaned, err := s.sanitizeChannelGroups(entry.AllowedChannelGroups)
		if err != nil {
			return config.APIKeyEntry{}, fmt.Errorf("%w: %v", ErrInvalidEntry, err)
		}
		entry.AllowedChannelGroups = cleaned
	}
	if s != nil && s.validateEntry != nil {
		if err := s.validateEntry(entry); err != nil {
			return config.APIKeyEntry{}, fmt.Errorf("%w: %v", ErrInvalidEntry, err)
		}
	}
	return entry, nil
}

func uniqueChannelGroups(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := internalrouting.NormalizeGroupName(value)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
