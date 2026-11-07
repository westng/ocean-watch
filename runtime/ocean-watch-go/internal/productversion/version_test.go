package productversion

import "testing"

func TestReleaseOrderAndMetadata(t *testing.T) {
	values := []string{"1.0.9", "1.1.0-alpha.1", "1.1.0-alpha.10", "1.1.0-beta.1", "1.1.0-rc.1", "1.1.0", "2.0.0-alpha.1"}
	for i := 1; i < len(values); i++ {
		if cmp, err := Compare(values[i-1], values[i]); err != nil || cmp != -1 {
			t.Fatalf("ordering %s / %s: %d, %v", values[i-1], values[i], cmp, err)
		}
	}
	if cmp, err := Compare("1.1.0-alpha.1+codex.old", "1.1.0-alpha.1+codex.new"); err != nil || cmp != 0 {
		t.Fatal("build metadata changed precedence")
	}
	for _, value := range []string{"1.1.0-alpha.0", "1.1.0-alpha.01", "01.1.0", "1.1.0-dev.1", "1.1.0a1", "1.1.0 ", "1.1.0+bad", "999999999999.0.0"} {
		if _, err := Parse(value); err == nil {
			t.Fatalf("accepted invalid version %q", value)
		}
	}
}
