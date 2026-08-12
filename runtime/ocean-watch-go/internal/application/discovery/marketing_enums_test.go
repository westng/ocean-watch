package discovery

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func TestMarketingDiscoverySDKEnumSnapshots(t *testing.T) {
	tests := []struct {
		name     string
		values   map[string]struct{}
		count    int
		checksum string
	}{
		{"external_action", externalActions, 156, "855adc5a708b4085722c1856680dac5183bbff9bf3d329177177009b412a5b37"},
		{"deep_external_action", deepExternalActions, 163, "3406f22d303874880c1a8156acc8cd84a3e81c734ee86c6f39fa40579f9e6847"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.values) != test.count {
				t.Fatalf("enum count = %d, want %d", len(test.values), test.count)
			}
			values := make([]string, 0, len(test.values))
			for value := range test.values {
				values = append(values, value)
			}
			sort.Strings(values)
			digest := sha256.Sum256([]byte(strings.Join(values, "\n") + "\n"))
			if got := fmt.Sprintf("%x", digest); got != test.checksum {
				t.Fatalf("enum checksum = %s, want %s", got, test.checksum)
			}
		})
	}
}

func TestMarketingGoalLandingTypesStayNarrowerThanDeepBidTypes(t *testing.T) {
	if _, ok := deepBidLandingTypes["ARTICLE"]; !ok {
		t.Fatal("deep-bid landing types lost ARTICLE")
	}
	if _, ok := goalLandingTypes["ARTICLE"]; ok {
		t.Fatal("optimized-goal landing types accepted unsupported ARTICLE")
	}
	for _, value := range []string{"APP", "DPA", "LINK", "MICRO_GAME", "NATIVE_ACTION", "QUICK_APP", "SHOP"} {
		if _, ok := goalLandingTypes[value]; !ok {
			t.Fatalf("optimized-goal landing types lost %s", value)
		}
	}
}
