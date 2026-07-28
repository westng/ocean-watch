package domain

import "testing"

func TestOAuthStateContract(t *testing.T) {
	tests := []struct {
		channel string
		state   string
	}{
		{channel: "marketing", state: "AD.fixture_nonce"},
		{channel: "qianchuan", state: "QC.fixture_nonce"},
	}
	for _, test := range tests {
		state, err := BuildOAuthState(test.channel, "fixture_nonce")
		if err != nil {
			t.Fatal(err)
		}
		if state != test.state {
			t.Fatalf("state = %q, want %q", state, test.state)
		}
		channel, err := ChannelFromOAuthState(state)
		if err != nil || channel != test.channel {
			t.Fatalf("state channel = %q, %v", channel, err)
		}
		if callbackErr := ValidateOAuthCallbackState(state, state, test.channel); callbackErr != nil {
			t.Fatal(callbackErr)
		}
	}
}

func TestOAuthStateRejectsMismatchAndWrongChannelBeforeExchange(t *testing.T) {
	if err := ValidateOAuthCallbackState("AD.other", "AD.expected", "marketing"); err == nil || err.Code != "state_mismatch" {
		t.Fatalf("unexpected mismatch result: %#v", err)
	}
	if err := ValidateOAuthCallbackState("QC.expected", "QC.expected", "marketing"); err == nil || err.Code != "state_channel_mismatch" {
		t.Fatalf("unexpected channel result: %#v", err)
	}
	for _, state := range []string{"", "AD", "XX.nonce", "AD.a.b"} {
		if _, err := ChannelFromOAuthState(state); err == nil {
			t.Fatalf("invalid state accepted: %q", state)
		}
	}
}
