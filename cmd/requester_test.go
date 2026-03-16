package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJoinURL(t *testing.T) {
	cases := []struct {
		base string
		path string
		want string
	}{
		{"https://example.com", "admin", "https://example.com/admin"},
		{"https://example.com/", "admin", "https://example.com/admin"},
		{"https://example.com", "/admin", "https://example.com/admin"},
		{"https://example.com/", "/admin", "https://example.com/admin"},
	}

	for _, tc := range cases {
		if got := joinURL(tc.base, tc.path); got != tc.want {
			t.Fatalf("joinURL(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

func TestParseFileTrimsAndSkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "payloads.txt")
	if err := os.WriteFile(filePath, []byte("foo\r\n\nbar\r\n"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	got, err := parseFile(filePath)
	if err != nil {
		t.Fatalf("parseFile error: %v", err)
	}

	if len(got) != 2 || got[0] != "foo" || got[1] != "bar" {
		t.Fatalf("unexpected parseFile result: %#v", got)
	}
}

func TestParsePayloadPositions(t *testing.T) {
	positions, template := parsePayloadPositions("http://example.com/§100§/user/§200§", "§")
	if len(positions) != 2 {
		t.Fatalf("expected 2 positions, got %d: %v", len(positions), positions)
	}
	if positions[0] != "100" || positions[1] != "200" {
		t.Errorf("unexpected positions: %v", positions)
	}
	// Template should contain internal placeholders for both positions
	if !strings.Contains(template, payloadPlaceholderPrefix) {
		t.Errorf("template should contain placeholder prefix, got: %q", template)
	}
	// Reconstruct original URL from template + positions
	reconstructed := template
	for i, val := range positions {
		reconstructed = strings.Replace(reconstructed, payloadPlaceholder(i), val, 1)
	}
	if reconstructed != "http://example.com/100/user/200" {
		t.Errorf("reconstructed URL mismatch: %q", reconstructed)
	}
}

func TestParsePayloadPositions_NoMarkers(t *testing.T) {
	positions, _ := parsePayloadPositions("http://example.com/admin", "§")
	if len(positions) != 0 {
		t.Errorf("expected 0 positions, got %d", len(positions))
	}
}

func TestSetupRequestOptionsParsesHeaders(t *testing.T) {
	opts := setupRequestOptions(
		"https://example.com/admin",
		"",
		"",
		[]string{"X-Test: one:two"},
		"",
		"payloads",
		"",
		false,
		[]string{"headers"},
		true,
		false,
		1000,
		false,
		false,
	)

	if len(opts.headers) != 2 {
		t.Fatalf("expected 2 headers, got %d", len(opts.headers))
	}

	if opts.headers[0].key != "User-Agent" || opts.headers[0].value != "nomore403" {
		t.Fatalf("unexpected default User-Agent header: %#v", opts.headers[0])
	}

	if opts.headers[1].key != "X-Test" || opts.headers[1].value != "one:two" {
		t.Fatalf("unexpected custom header: %#v", opts.headers[1])
	}
}
