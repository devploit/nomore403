package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadURLsFromInput_SingleURL(t *testing.T) {
	urls := readURLsFromInput("https://example.com/admin")
	if len(urls) != 1 || urls[0] != "https://example.com/admin" {
		t.Fatalf("expected single URL, got: %v", urls)
	}
}

func TestReadURLsFromInput_FileInput(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "urls.txt")
	content := "https://example.com/admin\nhttps://example.com/secret\n# comment\n\nhttps://example.com/api\n"
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	urls := readURLsFromInput(filePath)
	if len(urls) != 3 {
		t.Fatalf("expected 3 URLs, got %d: %v", len(urls), urls)
	}
	if urls[0] != "https://example.com/admin" {
		t.Errorf("url[0] = %q, want https://example.com/admin", urls[0])
	}
	if urls[2] != "https://example.com/api" {
		t.Errorf("url[2] = %q, want https://example.com/api", urls[2])
	}
}

func TestReadURLsFromInput_NonExistentFile(t *testing.T) {
	// A non-URL, non-file input should be returned as-is
	urls := readURLsFromInput("not-a-url-or-file")
	if len(urls) != 1 || urls[0] != "not-a-url-or-file" {
		t.Fatalf("expected input returned as-is, got: %v", urls)
	}
}

func TestOutputWriter_PlainText(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "output.txt")

	if err := initOutputWriter(outPath); err != nil {
		t.Fatalf("initOutputWriter: %v", err)
	}

	writeResultToOutput(Result{line: "GET /admin", statusCode: 200, contentLength: 1234}, "verb-tampering")
	writeResultToOutput(Result{line: "X-Forwarded-For: 127.0.0.1", statusCode: 403, contentLength: 500}, "headers")

	closeOutputWriter()

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "200") || !strings.Contains(content, "GET /admin") {
		t.Errorf("expected first result in output, got: %s", content)
	}
	if !strings.Contains(content, "403") || !strings.Contains(content, "X-Forwarded-For") {
		t.Errorf("expected second result in output, got: %s", content)
	}
}

func TestOutputWriter_JSON(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "output.json")

	// Enable JSON mode
	oldJSON := jsonOutput
	jsonOutput = true
	defer func() { jsonOutput = oldJSON }()

	// Reset jsonResults
	jsonResultsMutex.Lock()
	jsonResults = nil
	jsonResultsMutex.Unlock()

	if err := initOutputWriter(outPath); err != nil {
		t.Fatalf("initOutputWriter: %v", err)
	}

	writeResultToOutput(Result{line: "GET /admin", statusCode: 200, contentLength: 1234, score: 90, likelihood: "high", reproCurl: "curl ..."}, "verb-tampering")
	writeResultToOutput(Result{line: "X-Forwarded-For: 127.0.0.1", statusCode: 403, contentLength: 500, score: 20, likelihood: "low"}, "headers")

	closeOutputWriter()

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	var results []JSONResult
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("unmarshal JSON: %v (content: %s)", err, string(data))
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].StatusCode != 200 || results[0].Technique != "verb-tampering" {
		t.Errorf("unexpected first result: %+v", results[0])
	}
	if results[0].Score != 90 || results[0].Likelihood != "high" || results[0].ReproCurl != "curl ..." {
		t.Errorf("expected enriched fields in first result, got %+v", results[0])
	}
	if results[1].StatusCode != 403 || results[1].Technique != "headers" {
		t.Errorf("unexpected second result: %+v", results[1])
	}
}

func TestOutputWriter_JSONL(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "output.jsonl")

	oldJSONL := jsonLines
	jsonLines = true
	defer func() { jsonLines = oldJSONL }()

	jsonResultsMutex.Lock()
	jsonResults = nil
	jsonResultsMutex.Unlock()

	if err := initOutputWriter(outPath); err != nil {
		t.Fatalf("initOutputWriter: %v", err)
	}

	writeResultToOutput(Result{line: "GET /admin", statusCode: 200, contentLength: 1234, score: 88, likelihood: "high"}, "verb-tampering")
	writeResultToOutput(Result{line: "X-Original-URL: /", statusCode: 302, contentLength: 12, score: 67, likelihood: "medium"}, "header-confusion")
	closeOutputWriter()

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 jsonl lines, got %d", len(lines))
	}

	var first JSONResult
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("unmarshal first jsonl line: %v", err)
	}
	if first.Score != 88 || first.Likelihood != "high" {
		t.Fatalf("unexpected first jsonl result: %+v", first)
	}
}
