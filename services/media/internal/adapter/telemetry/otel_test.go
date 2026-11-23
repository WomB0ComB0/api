package telemetry

import "testing"

func TestResolveTracesURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://api.axiom.co", "https://api.axiom.co/v1/traces"},
		{"https://api.axiom.co/", "https://api.axiom.co/v1/traces"},
		{"https://api.axiom.co/v1/traces", "https://api.axiom.co/v1/traces"},
		{"http://localhost:4318", "http://localhost:4318/v1/traces"},
	}
	for _, tc := range cases {
		if got := resolveTracesURL(tc.in); got != tc.want {
			t.Fatalf("resolveTracesURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseOTLPHeaders(t *testing.T) {
	got := ParseOTLPHeaders("Authorization=Bearer%20xaat-abc123, X-Axiom-Dataset=api-traces")
	if got["Authorization"] != "Bearer xaat-abc123" {
		t.Fatalf("unexpected Authorization header: %q", got["Authorization"])
	}
	if got["X-Axiom-Dataset"] != "api-traces" {
		t.Fatalf("unexpected X-Axiom-Dataset header: %q", got["X-Axiom-Dataset"])
	}
}
