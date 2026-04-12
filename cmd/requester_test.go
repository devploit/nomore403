package cmd

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFormatPrintedResultIncludesTechniqueAlias(t *testing.T) {
	result := Result{
		technique:     "absolute-uri",
		statusCode:    403,
		contentLength: 236,
		line:          "request-target: https://example.com/admin",
		score:         26,
		likelihood:    "low",
	}

	got := formatPrintedResult(result)
	if !strings.Contains(got, "abs-uri") {
		t.Fatalf("expected technique alias in row, got %q", got)
	}
	if strings.Contains(got, "->") {
		t.Fatalf("expected row to show only final status code, got %q", got)
	}
	if strings.Contains(got, "LOW") || strings.Contains(got, "[") {
		t.Fatalf("expected compact score without legacy label, got %q", got)
	}
	if !strings.Contains(got, "26.") || !strings.Contains(got, "403") || !strings.Contains(got, "236 bytes") {
		t.Fatalf("expected compact score, status and size columns, got %q", got)
	}
}

func TestFormatPrintedResultKeepsClearColumnSpacing(t *testing.T) {
	result := Result{
		technique:     "path-normalization",
		statusCode:    400,
		contentLength: 122,
		line:          "https://example.com/%2e%2e/admin",
		score:         16,
		likelihood:    "low",
	}

	got := formatPrintedResult(result)
	if !strings.Contains(got, "400") {
		t.Fatalf("expected padded status column, got %q", got)
	}
	if !strings.Contains(got, "122 bytes") {
		t.Fatalf("expected padded bytes column, got %q", got)
	}
	if !strings.Contains(got, "bytes    https://") {
		t.Fatalf("expected extra spacing between bytes and payload, got %q", got)
	}
}

func TestFormatPrintedResultKeepsWideByteColumnAligned(t *testing.T) {
	result := Result{
		technique:     "headers-ip",
		statusCode:    200,
		contentLength: 47968,
		line:          "Host: 0.0.0.0",
		score:         100,
		likelihood:    "high",
	}

	got := formatPrintedResult(result)
	if !strings.Contains(got, "47968 bytes    Host: 0.0.0.0") {
		t.Fatalf("expected wide byte value to keep padding before payload, got %q", got)
	}
}

func TestFormatPrintedResultDefaultRequestUsesBaselineLabel(t *testing.T) {
	result := Result{
		technique:     "default",
		statusCode:    403,
		contentLength: 520,
		line:          "https://example.com/admin",
		defaultReq:    true,
		score:         4,
		likelihood:    "low",
	}

	got := formatPrintedResult(result)
	if !strings.Contains(got, "default") {
		t.Fatalf("expected default technique alias, got %q", got)
	}
	if strings.Contains(got, "baseline") {
		t.Fatalf("expected baseline marker to move to section header, got %q", got)
	}
}

func TestEnsureResultsSectionsPrintOnce(t *testing.T) {
	printedBaselineHeader = false
	printedFindingsHeader = false
	oldVerbose := getVerbose()
	setVerbose(false)
	defer setVerbose(oldVerbose)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	ensureResultsSectionLocked(true)
	ensureResultsSectionLocked(true)
	ensureResultsSectionLocked(false)
	ensureResultsSectionLocked(false)

	_ = w.Close()
	out, _ := io.ReadAll(r)
	got := string(out)
	if strings.Count(got, "BASELINE") != 1 || strings.Count(got, "FINDINGS") != 1 {
		t.Fatalf("expected baseline/findings headers once, got %q", got)
	}
}

func TestFormatSectionHeaderUsesConsistentWidth(t *testing.T) {
	got := formatSectionHeader("HEADERS")
	if utf8.RuneCountInString(got) != 40 {
		t.Fatalf("expected consistent width 40, got %d in %q", utf8.RuneCountInString(got), got)
	}
}

func TestGenerateCaseCombinationsHandlesUnicodeAndNonLetters(t *testing.T) {
	got := generateCaseCombinations("ñ-1")
	want := []string{"ñ-1", "Ñ-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected combinations: got %v want %v", got, want)
	}
}

func TestBuildCurlVersionInvocationPreservesMethodAndHeaders(t *testing.T) {
	args, display := buildCurlVersionInvocation(
		"HEAD",
		"https://example.com/admin",
		[]header{
			{"User-Agent", "agent/1.0"},
			{"Host", "alt.example.com"},
		},
		"http://127.0.0.1:8080",
		"--http2",
		true,
	)

	wantArgs := []string{
		"-i", "-s", "--http2",
		"-X", "HEAD",
		"-H", "User-Agent: agent/1.0",
		"-H", "Host: alt.example.com",
		"-x", "http://127.0.0.1:8080",
		"-L",
		"--insecure",
		"https://example.com/admin",
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected curl args:\n got: %v\nwant: %v", args, wantArgs)
	}
	if !strings.Contains(display, "-X 'HEAD'") || !strings.Contains(display, "'Host: alt.example.com'") {
		t.Fatalf("display command lost request parity: %q", display)
	}
}

func TestBuildCurlParserInvocationUsesMinimalHeaders(t *testing.T) {
	args, display := buildCurlParserInvocation("GET", "https://example.com/admin", "", false)
	joined := strings.Join(args, " ")
	for _, headerName := range []string{"User-Agent:", "Accept:", "Connection:", "Host:"} {
		if !strings.Contains(joined, headerName) {
			t.Fatalf("expected stripped parser header %q in args: %v", headerName, args)
		}
	}
	if !strings.Contains(display, "'User-Agent:'") {
		t.Fatalf("expected parser display command to show stripped headers, got %q", display)
	}
}

func TestBuildAbsoluteURIPayloadsAvoidsMalformedDoubleSlash(t *testing.T) {
	parsed, err := url.Parse("https://example.com/web.config")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	got := buildAbsoluteURIPayloads(parsed, parsed.RequestURI())
	want := []string{
		"https://example.com/web.config",
		"https://anything@example.com/web.config",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected payloads:\n got: %v\nwant: %v", got, want)
	}
	for _, payload := range got {
		if strings.Contains(payload, "//web.config") {
			t.Fatalf("malformed payload still present: %q", payload)
		}
	}
}

func TestGlobalBaselineFallbackKeepsExtendedResponseData(t *testing.T) {
	resetMaps()
	setGlobalBaseline(ResponseInfo{
		statusCode:    403,
		contentLength: 520,
		bodyHash:      "abc",
		contentType:   "text/html",
	})

	got := baselineForTechnique("headers-ip")
	if got.bodyHash != "abc" || got.contentType != "text/html" {
		t.Fatalf("expected full baseline fallback, got %#v", got)
	}
}

func TestMarkTechniqueProducedPromotesHeadersUmbrella(t *testing.T) {
	resetMaps()
	markTechniqueProduced("headers-ip")
	if !producedTechniques["headers-ip"] || !producedTechniques["headers"] {
		t.Fatalf("expected headers subfamily to promote umbrella, got %#v", producedTechniques)
	}
}

func TestMarkTechniqueProducedKeepsHTTPParserSeparate(t *testing.T) {
	resetMaps()
	markTechniqueProduced("http-parser")
	if !producedTechniques["http-parser"] {
		t.Fatalf("expected http-parser to be marked as produced")
	}
	if producedTechniques["http-versions"] {
		t.Fatalf("did not expect http-parser to mark http-versions as produced")
	}
}

func TestParseCurlOutputExtractsBodyAndHeaders(t *testing.T) {
	output := strings.Join([]string{
		"HTTP/1.1 302 Found",
		"Location: /login",
		"Content-Type: text/html; charset=utf-8",
		"Server: envoy",
		"",
		"redirect-body",
	}, "\r\n")

	got := parseCurlOutput(output, "--http2")
	if got.statusCode != 302 {
		t.Fatalf("expected status 302, got %d", got.statusCode)
	}
	if got.line != "HTTP/2" {
		t.Fatalf("expected version label HTTP/2, got %q", got.line)
	}
	if got.contentLength != len("redirect-body") {
		t.Fatalf("expected body length %d, got %d", len("redirect-body"), got.contentLength)
	}
	if got.location != "/login" || got.contentType != "text/html; charset=utf-8" || got.server != "envoy" {
		t.Fatalf("expected parsed headers, got %#v", got)
	}
	if got.bodyHash == "" {
		t.Fatalf("expected body hash to be populated")
	}
}

func TestPrintTopFindingsSkipsWhenLimitIsZero(t *testing.T) {
	resetMaps()
	oldVerbose := getVerbose()
	setVerbose(false)
	defer setVerbose(oldVerbose)
	topFindings = []Result{{technique: "headers", score: 90, likelihood: "high", contentLength: 10}}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	printTopFindings(0)

	_ = w.Close()
	out, _ := io.ReadAll(r)
	if len(out) != 0 {
		t.Fatalf("expected no summary output for --top 0, got %q", string(out))
	}
}

func TestCollapseFindingFamiliesKeepsDifferentTechniquesSeparate(t *testing.T) {
	findings := []Result{
		{technique: "headers-ip", familyKey: "same", statusCode: 200, contentLength: 100},
		{technique: "http-parser", familyKey: "same", statusCode: 200, contentLength: 100},
	}

	got := collapseFindingFamilies(findings)
	if len(got) != 2 {
		t.Fatalf("expected different techniques to survive collapse independently, got %d", len(got))
	}
}

func TestCrossTechniqueSuppressionStillRecordsTopFindings(t *testing.T) {
	resetMaps()
	setVerbose(false)
	setDefaultSc(403)
	setDefaultRespCl(100)
	topScoreMin = 55
	variationScoreMin = 25

	first := Result{
		line:          "Host: 0.0.0.0",
		statusCode:    200,
		contentLength: 5000,
		bodyHash:      "aaa",
		contentType:   "text/html",
	}
	second := Result{
		line:          "minimal curl request",
		statusCode:    200,
		contentLength: 5000,
		bodyHash:      "aaa",
		contentType:   "text/html",
	}

	printResponse(first, "headers-ip")
	printResponse(second, "http-parser")

	if len(topFindings) != 2 {
		t.Fatalf("expected both high-signal findings to be recorded for summaries, got %d", len(topFindings))
	}
}

func TestHTTPParserResultsAreRecordedForTopFindings(t *testing.T) {
	resetMaps()
	setVerbose(false)
	setDefaultSc(403)
	setDefaultRespCl(134)

	result := Result{
		line:          "minimal curl request",
		statusCode:    200,
		contentLength: 47968,
		bodyHash:      "bbb",
		contentType:   "text/html",
	}

	printResponse(result, "http-parser")

	if len(topFindings) != 1 {
		t.Fatalf("expected http-parser result to be recorded for summaries, got %d", len(topFindings))
	}
	if topFindings[0].technique != "http-parser" {
		t.Fatalf("expected recorded technique http-parser, got %q", topFindings[0].technique)
	}
}

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
