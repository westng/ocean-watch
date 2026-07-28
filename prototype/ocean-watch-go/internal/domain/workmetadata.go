package domain

import (
	"errors"
	"net/url"
	"strings"
)

const workMetadataIntegrationKey = "qianchuan_work_metadata"

func ValidateWorkMetadataEndpoint(value string) (string, error) {
	endpoint := strings.TrimSpace(value)
	if endpoint == "" {
		return "", nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		if strings.Contains(err.Error(), "invalid port") {
			return "", errors.New("Qianchuan work metadata endpoint has an invalid port")
		}
		return "", errors.New("Qianchuan work metadata endpoint must be a credential-free HTTPS URL")
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("Qianchuan work metadata endpoint must be a credential-free HTTPS URL")
	}
	return parsed.String(), nil
}

func WorkMetadataEndpoint(config map[string]any) (string, error) {
	integrations, _ := config["integrations"].(map[string]any)
	section, _ := integrations[workMetadataIntegrationKey].(map[string]any)
	endpoint, _ := section["endpoint"].(string)
	return ValidateWorkMetadataEndpoint(endpoint)
}

func WorkMetadataConfigured(config map[string]any) bool {
	integrations, _ := config["integrations"].(map[string]any)
	section, _ := integrations[workMetadataIntegrationKey].(map[string]any)
	endpoint, _ := section["endpoint"].(string)
	return strings.TrimSpace(endpoint) != ""
}

func SetWorkMetadataEndpoint(config map[string]any, endpoint string) error {
	validated, err := ValidateWorkMetadataEndpoint(endpoint)
	if err != nil {
		return err
	}
	if validated == "" {
		return errors.New("--endpoint cannot be empty")
	}
	integrations, exists := config["integrations"]
	if !exists {
		integrations = map[string]any{}
		config["integrations"] = integrations
	}
	sections, ok := integrations.(map[string]any)
	if !ok {
		return errors.New("config integrations must be an object")
	}
	sections[workMetadataIntegrationKey] = map[string]any{"endpoint": validated}
	return nil
}

func ClearWorkMetadataEndpoint(config map[string]any) {
	integrations, ok := config["integrations"].(map[string]any)
	if !ok {
		return
	}
	delete(integrations, workMetadataIntegrationKey)
	if len(integrations) == 0 {
		delete(config, "integrations")
	}
}
