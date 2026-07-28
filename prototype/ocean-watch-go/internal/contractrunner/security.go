package contractrunner

import (
	"fmt"
	"regexp"
	"strings"
)

var evidencePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{12,}`),
	regexp.MustCompile(`(?i)(?:access|refresh)[_-]?token["']?\s*[:=]\s*["']?([A-Za-z0-9._~+/=-]{12,})`),
	regexp.MustCompile(`(?i)(?:app[_-]?secret|client[_-]?secret|auth[_-]?code)["']?\s*[:=]\s*["']?([A-Za-z0-9._~+/=-]{12,})`),
	regexp.MustCompile(`(?i)https?://[^\s"']*(?:mcp|streamable)[^\s"']*`),
}

var evidenceAllowlist = []string{
	"TEST_ACCESS_TOKEN_DO_NOT_USE",
	"TEST_REFRESH_TOKEN_DO_NOT_USE",
	"TEST_APP_SECRET_DO_NOT_USE",
	"TEST_AUTH_CODE_DO_NOT_USE",
}

func validateEvidence(payload []byte) error {
	text := string(payload)
	for _, pattern := range evidencePatterns {
		for _, match := range pattern.FindAllString(text, -1) {
			allowed := false
			for _, fixture := range evidenceAllowlist {
				if strings.Contains(match, fixture) {
					allowed = true
					break
				}
			}
			if !allowed {
				return fmt.Errorf("contract evidence contains credential-like material matching %s", pattern.String())
			}
		}
	}
	return nil
}
