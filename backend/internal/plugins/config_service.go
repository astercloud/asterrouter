package plugins

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func (s *Service) Config(ctx context.Context, id string) (Config, error) {
	plugin, ok, err := s.repo.FindPlugin(ctx, strings.TrimSpace(id))
	if err != nil {
		return Config{}, err
	}
	if !ok {
		return Config{}, ErrPluginNotFound
	}
	record, ok, err := s.repo.FindConfig(ctx, plugin.ID)
	if err != nil {
		return Config{}, err
	}
	if !ok {
		now := time.Now().UTC()
		return Config{PluginID: plugin.ID, Settings: map[string]string{}, SecretHints: map[string]string{}, CreatedAt: now, UpdatedAt: now}, nil
	}
	return configFromRecord(record), nil
}

func (s *Service) UpdateConfig(ctx context.Context, id string, req ConfigRequest) (Config, error) {
	plugin, ok, err := s.repo.FindPlugin(ctx, strings.TrimSpace(id))
	if err != nil {
		return Config{}, err
	}
	if !ok {
		return Config{}, ErrPluginNotFound
	}
	if !plugin.Configurable {
		return Config{}, ErrPluginNotConfigurable
	}
	if plugin.ID == ArtifactS3SinkPluginID {
		return Config{}, fmt.Errorf("%w: use artifact sink destination configuration", ErrPluginConfigInvalid)
	}
	if plugin.Status == StatusLocked {
		return Config{}, ErrPluginLocked
	}
	if err := validateConfigRequest(plugin, req); err != nil {
		return Config{}, err
	}

	now := time.Now().UTC()
	record, ok, err := s.repo.FindConfig(ctx, plugin.ID)
	if err != nil {
		return Config{}, err
	}
	if !ok {
		record = configRecord{
			PluginID:          plugin.ID,
			Settings:          map[string]string{},
			SecretCiphertexts: map[string]string{},
			SecretHints:       map[string]string{},
			CreatedAt:         now,
		}
	}
	record.Settings = cleanStringMap(req.Settings)
	if record.SecretCiphertexts == nil {
		record.SecretCiphertexts = map[string]string{}
	}
	if record.SecretHints == nil {
		record.SecretHints = map[string]string{}
	}
	for key, value := range cleanStringMap(req.Secrets) {
		if value == "" {
			continue
		}
		ciphertext, err := encryptSecret(s.secretKey, value)
		if err != nil {
			return Config{}, err
		}
		record.SecretCiphertexts[key] = ciphertext
		record.SecretHints[key] = maskSecret(value)
	}
	record.UpdatedAt = now
	if err := s.repo.SaveConfig(ctx, record); err != nil {
		return Config{}, err
	}
	return configFromRecord(record), nil
}

func configFromRecord(record configRecord) Config {
	return Config{
		PluginID:    record.PluginID,
		Settings:    cloneStringMap(record.Settings),
		SecretHints: cloneStringMap(record.SecretHints),
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}
}

func validateConfigRequest(plugin Plugin, req ConfigRequest) error {
	settings := cleanStringMap(req.Settings)
	secrets := cleanStringMap(req.Secrets)
	if plugin.Category == "notification" {
		if severity := strings.TrimSpace(settings["min_severity"]); severity != "" && severityRank(severity) == 0 {
			return fmt.Errorf("%w: min_severity must be info, warning, or critical", ErrPluginConfigInvalid)
		}
	}
	if plugin.Category == "notification" && hasNotificationWebhookAdapter(plugin.ID) {
		if webhookURL := strings.TrimSpace(secrets["webhook_url"]); webhookURL != "" {
			parsed, err := url.Parse(webhookURL)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("%w: webhook_url must be an HTTP or HTTPS URL", ErrPluginConfigInvalid)
			}
		}
	}
	return nil
}
