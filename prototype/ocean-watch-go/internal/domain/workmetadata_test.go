package domain

import "testing"

func TestValidateWorkMetadataEndpointRequiresCredentialFreeHTTPS(t *testing.T) {
	for _, endpoint := range []string{
		"http://metadata.example.test/api",
		"https://user:pass@metadata.example.test/api",
		"https://metadata.example.test/api#fragment",
	} {
		if _, err := ValidateWorkMetadataEndpoint(endpoint); err == nil {
			t.Fatalf("accepted unsafe endpoint %q", endpoint)
		}
	}
	want := "https://metadata.example.test/api?version=1"
	got, err := ValidateWorkMetadataEndpoint(want)
	if err != nil || got != want {
		t.Fatalf("validated endpoint = %q, %v", got, err)
	}
}

func TestWorkMetadataMutationPreservesUnrelatedConfig(t *testing.T) {
	config := map[string]any{"future": map[string]any{"preserved": true}}
	if err := SetWorkMetadataEndpoint(config, "https://metadata.example.test/api"); err != nil {
		t.Fatal(err)
	}
	if config["future"].(map[string]any)["preserved"] != true {
		t.Fatal("setting endpoint lost unrelated config")
	}
	endpoint, err := WorkMetadataEndpoint(config)
	if err != nil || endpoint != "https://metadata.example.test/api" {
		t.Fatalf("stored endpoint = %q, %v", endpoint, err)
	}
	ClearWorkMetadataEndpoint(config)
	if _, exists := config["integrations"]; exists {
		t.Fatal("empty integrations object was retained")
	}
}
