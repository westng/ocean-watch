package official

import (
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
)

// Distinct budget fields are deliberately checked against the same frozen
// query descriptions that supply units for the public MCP projection.
func TestQianchuanBudgetUnitsByResponseField(t *testing.T) {
	for _, test := range []struct {
		query, unit string
		paths       []string
	}{
		{"qianchuan_account_budget_get_v1", "CNY", []string{"data.budget"}},
		{"qianchuan_suggest_budget_v1", "CNY/100000", []string{"data.suggest_budget_low", "data.suggest_budget_high"}},
		{"qianchuan_uni_promotion_list_v1", "", []string{"data.ad_list.ad_info.budget"}},
		{"qianchuan_uni_promotion_ad_detail_v1", "", []string{"data.delivery_setting.budget"}},
		{"qianchuan_uni_aweme_suggest_budget_v1", "", []string{"data.budget", "data.budget_min", "data.budget_max", "data.budget_danger", "data.suggested_budget_list", "data.estimate_trans_cost_list.budget"}},
	} {
		query, ok, err := contracts.ApprovedOfficialQueryByID(test.query)
		if !ok || err != nil {
			t.Fatalf("%s: query unavailable: %v", test.query, err)
		}
		units := map[string]string{}
		for _, field := range query.OutputFields {
			units[field.Path] = fieldUnit(field, query.OutputFields)
		}
		for _, path := range test.paths {
			if unit, found := units[path]; !found || unit != test.unit {
				t.Errorf("%s %s: found=%v unit=%q, want %q", test.query, path, found, unit, test.unit)
			}
		}
	}
}
