package cmd

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// testServer creates an httptest server that records all incoming requests.
// Returns the server, a function to get recorded requests, and a cleanup function.
func testServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, func() []*http.Request) {
	t.Helper()
	var mu sync.Mutex
	var requests []*http.Request

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Clone(r.Context()))
		mu.Unlock()
		if handler != nil {
			handler(w, r)
		} else {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, "Forbidden")
		}
	}))

	getRequests := func() []*http.Request {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]*http.Request, len(requests))
		copy(cp, requests)
		return cp
	}

	return ts, getRequests
}

// setupPayloadsDir creates a temporary payloads directory with test data.
func setupPayloadsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		"httpmethods":   "GET\nPOST\nPUT\nDELETE\n",
		"headers":       "X-Forwarded-For\nX-Real-IP\n",
		"ips":           "127.0.0.1\n10.0.0.1\n",
		"simpleheaders": "X-Custom-Header value1\nX-Another test\n",
		"endpaths":      "/\n//\n/..\n",
		"midpaths":      "%2e/\n..;/\n",
		"useragents":    "TestAgent/1.0\nTestAgent/2.0\n",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	return dir
}

// resetTestState resets global state between tests.
func resetTestState() {
	setVerbose(true) // Show all results in tests
	setDefaultCl(0)
	setCalibTolerance(0)
	setDefaultSc(0)
	setDefaultRespCl(0)
	maxGoroutines = 5
	delay = 0
	redirect = false
	statusCodes = nil
	uniqueOutput = false
	nobanner = true
	jsonOutput = false
	payloadPosition = ""
	outputWriter = nil
	jsonResultsMutex.Lock()
	jsonResults = nil
	jsonResultsMutex.Unlock()
	resetMaps()
}

func TestVerbTamperingSendsCorrectMethods(t *testing.T) {
	resetTestState()

	ts, getRequests := testServer(t, nil)
	defer ts.Close()

	payloadsDir := setupPayloadsDir(t)

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		folder:    payloadsDir,
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestMethods(opts)

	reqs := getRequests()
	if len(reqs) == 0 {
		t.Fatal("expected requests to be sent, got 0")
	}

	methods := make(map[string]bool)
	for _, r := range reqs {
		methods[r.Method] = true
	}

	expectedMethods := []string{"GET", "POST", "PUT", "DELETE"}
	for _, m := range expectedMethods {
		if !methods[m] {
			t.Errorf("expected method %s to be sent, but it wasn't. Got methods: %v", m, methods)
		}
	}
}

func TestHeadersBypassSendsCorrectHeaders(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var capturedHeaders []http.Header

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedHeaders = append(capturedHeaders, r.Header.Clone())
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	payloadsDir := setupPayloadsDir(t)

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		folder:    payloadsDir,
		bypassIP:  "127.0.0.1",
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestHeaders(opts)

	mu.Lock()
	defer mu.Unlock()

	if len(capturedHeaders) == 0 {
		t.Fatal("expected header bypass requests to be sent, got 0")
	}

	// Check that at least some requests contain bypass headers
	foundBypassHeader := false
	for _, h := range capturedHeaders {
		if h.Get("X-Forwarded-For") == "127.0.0.1" || h.Get("X-Real-Ip") == "127.0.0.1" {
			foundBypassHeader = true
			break
		}
	}
	if !foundBypassHeader {
		t.Error("expected at least one request with X-Forwarded-For or X-Real-IP bypass header")
	}

	// Check that simple headers are also sent
	foundSimpleHeader := false
	for _, h := range capturedHeaders {
		if h.Get("X-Custom-Header") == "value1" {
			foundSimpleHeader = true
			break
		}
	}
	if !foundSimpleHeader {
		t.Error("expected at least one request with simple header X-Custom-Header")
	}
}

func TestEndPathsSendsCorrectPaths(t *testing.T) {
	resetTestState()

	ts, getRequests := testServer(t, nil)
	defer ts.Close()

	payloadsDir := setupPayloadsDir(t)

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		folder:    payloadsDir,
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestEndPaths(opts)

	reqs := getRequests()
	if len(reqs) == 0 {
		t.Fatal("expected endpath requests to be sent, got 0")
	}

	paths := make([]string, 0, len(reqs))
	for _, r := range reqs {
		paths = append(paths, r.URL.Path)
	}

	// Check that modified paths were sent (not just the original /admin)
	foundModified := false
	for _, p := range paths {
		if p != "/admin" && strings.Contains(p, "admin") {
			foundModified = true
			break
		}
	}
	if !foundModified {
		t.Errorf("expected modified paths, got: %v", paths)
	}
}

func TestMidPathsSendsCorrectPaths(t *testing.T) {
	resetTestState()

	ts, getRequests := testServer(t, nil)
	defer ts.Close()

	payloadsDir := setupPayloadsDir(t)

	opts := RequestOptions{
		uri:       ts.URL + "/secret/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		folder:    payloadsDir,
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestMidPaths(opts)

	reqs := getRequests()
	if len(reqs) == 0 {
		t.Fatal("expected midpath requests to be sent, got 0")
	}

	// Midpaths should inject path modifiers before the last segment
	foundMidpath := false
	for _, r := range reqs {
		raw := r.URL.RawPath
		if raw == "" {
			raw = r.URL.Path
		}
		// Should contain the midpath payload between /secret/ and admin
		if strings.Contains(raw, "secret") && strings.Contains(raw, "admin") {
			foundMidpath = true
			break
		}
	}
	if !foundMidpath {
		t.Error("expected midpath-modified requests containing both 'secret' and 'admin' segments")
	}
}

func TestDoubleEncodingSendsEncodedPaths(t *testing.T) {
	resetTestState()

	ts, getRequests := testServer(t, nil)
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestDoubleEncoding(opts)

	reqs := getRequests()
	if len(reqs) == 0 {
		t.Fatal("expected double-encoding requests to be sent, got 0")
	}

	// Double-encoded paths should contain percent-encoded characters
	foundEncoded := false
	for _, r := range reqs {
		raw := r.URL.RawPath
		if raw == "" {
			raw = r.URL.Path
		}
		if strings.Contains(raw, "%25") || strings.Contains(raw, "%") {
			foundEncoded = true
			break
		}
	}
	if !foundEncoded {
		t.Error("expected at least one request with double-encoded path characters")
	}
}

func TestPathCaseSwitchingSendsVariations(t *testing.T) {
	resetTestState()

	ts, getRequests := testServer(t, nil)
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestPathCaseSwitching(opts)

	reqs := getRequests()
	if len(reqs) == 0 {
		t.Fatal("expected path case switching requests to be sent, got 0")
	}

	// Should contain case variations of "admin"
	foundVariation := false
	for _, r := range reqs {
		path := strings.ToLower(r.URL.Path)
		if strings.Contains(path, "admin") && r.URL.Path != "/admin" {
			foundVariation = true
			break
		}
	}
	if !foundVariation {
		paths := make([]string, 0, len(reqs))
		for _, r := range reqs {
			paths = append(paths, r.URL.Path)
		}
		t.Errorf("expected case variations of /admin, got: %v", paths)
	}
}

func TestRateLimitDoesNotCrash(t *testing.T) {
	resetTestState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "Rate limited")
	}))
	defer ts.Close()

	payloadsDir := setupPayloadsDir(t)

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		folder:    payloadsDir,
		timeout:   5000,
		rateLimit: true,
		redirect:  false,
		verbose:   true,
	}

	// This should NOT crash (previously would log.Fatalf)
	requestMethods(opts)
	requestHeaders(opts)
	requestEndPaths(opts)
}

func TestMissingPayloadFileDoesNotCrash(t *testing.T) {
	resetTestState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	emptyDir := t.TempDir()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		folder:    emptyDir,
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	// None of these should crash despite missing payload files
	requestMethods(opts)
	requestMethodsCaseSwitching(opts)
	requestHeaders(opts)
	requestEndPaths(opts)
	requestMidPaths(opts)
}

func TestValidateURI(t *testing.T) {
	cases := []struct {
		uri     string
		wantErr bool
	}{
		{"https://example.com/admin", false},
		{"http://example.com/", false},
		{"", true},
		{"ftp://example.com", true},
		{"not-a-url", true},
		{"://missing-scheme", true},
	}

	for _, tc := range cases {
		err := validateURI(tc.uri)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateURI(%q): got err=%v, wantErr=%v", tc.uri, err, tc.wantErr)
		}
	}
}

func TestIsCalibrationMatch(t *testing.T) {
	setVerbose(false)
	setDefaultCl(1000)
	setCalibTolerance(50)
	setFragmentCl(0)

	cases := []struct {
		cl   int
		want bool
	}{
		{1000, true},  // exact match
		{1025, true},  // within tolerance
		{975, true},   // within tolerance
		{1050, true},  // at tolerance boundary
		{950, true},   // at tolerance boundary
		{1051, false}, // outside tolerance
		{949, false},  // outside tolerance
		{500, false},  // way outside
	}

	for _, tc := range cases {
		got := isCalibrationMatch(tc.cl)
		if got != tc.want {
			t.Errorf("isCalibrationMatch(%d) with default=1000, tolerance=50: got %v, want %v", tc.cl, got, tc.want)
		}
	}

	// Test with zero default (disabled calibration)
	setDefaultCl(0)
	if isCalibrationMatch(1000) {
		t.Error("isCalibrationMatch should return false when defaultCl is 0")
	}

	// Test fragment baseline matching
	setDefaultCl(1000)
	setFragmentCl(2000)
	if !isCalibrationMatch(2000) {
		t.Error("isCalibrationMatch should match fragment baseline")
	}
	if !isCalibrationMatch(2025) {
		t.Error("isCalibrationMatch should match within tolerance of fragment baseline")
	}
	if isCalibrationMatch(2051) {
		t.Error("isCalibrationMatch should not match outside tolerance of fragment baseline")
	}
	setFragmentCl(0)

	// Test verbose mode bypasses calibration
	setVerbose(true)
	setDefaultCl(1000)
	if isCalibrationMatch(1000) {
		t.Error("isCalibrationMatch should return false in verbose mode")
	}
	setVerbose(false)
}

func TestIsTransientError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("connection refused"), true},
		{fmt.Errorf("i/o timeout"), true},
		{fmt.Errorf("unexpected EOF"), true},
		{fmt.Errorf("connection reset by peer"), true},
		{fmt.Errorf("invalid URL"), false},
		{fmt.Errorf("malformed response"), false},
	}

	for _, tc := range cases {
		got := isTransientError(tc.err)
		if got != tc.want {
			t.Errorf("isTransientError(%v): got %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestAutoCalibrationMultiSample(t *testing.T) {
	resetTestState()

	requestCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusNotFound)
		// Return slightly varying response sizes to test tolerance
		body := strings.Repeat("x", 100+requestCount)
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   false,
	}

	avgCl, tolerance := runAutocalibrate(opts)

	if avgCl == 0 {
		t.Fatal("expected non-zero average content length from calibration")
	}

	if tolerance < calibrationTolerance {
		t.Errorf("expected tolerance >= %d, got %d", calibrationTolerance, tolerance)
	}
}

func TestVerbCaseSwitchingSendsVariations(t *testing.T) {
	resetTestState()

	ts, getRequests := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Return different content for different methods to trigger verbTamperingResults
		if r.Method == "POST" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "OK - POST works")
		} else {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, "Forbidden")
		}
	})
	defer ts.Close()

	payloadsDir := setupPayloadsDir(t)

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		folder:    payloadsDir,
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	// First run verb tampering to populate verbTamperingResults
	requestMethods(opts)

	initialReqs := len(getRequests())
	if initialReqs == 0 {
		t.Fatal("verb tampering should have sent requests")
	}

	// Now run case switching
	requestMethodsCaseSwitching(opts)

	allReqs := getRequests()
	caseSwitchReqs := allReqs[initialReqs:]

	if len(caseSwitchReqs) == 0 {
		// This might happen if no verb tampering result differed — that's okay,
		// but let's verify verbTamperingResults was populated
		if len(verbTamperingResults) == 0 {
			t.Log("No verb tampering results differed from default, so no case switching was done (expected)")
			return
		}
		t.Fatal("verbTamperingResults not empty but no case switch requests were sent")
	}

	// Check that case variations were actually sent
	methods := make(map[string]bool)
	for _, r := range caseSwitchReqs {
		methods[r.Method] = true
	}

	if len(methods) <= 1 {
		t.Errorf("expected multiple method case variations, got: %v", methods)
	}
}

func TestRequestFilePreservesQueryString(t *testing.T) {
	resetTestState()

	// Create a mock server that records the full request URI
	var mu sync.Mutex
	var capturedURIs []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedURIs = append(capturedURIs, r.URL.RequestURI())
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	}))
	defer ts.Close()

	// Build a raw HTTP request with query parameters (Burp-style)
	rawRequest := fmt.Sprintf("GET /upload?action=get_config_data&user_id=239501342 HTTP/1.1\r\nHost: %s\r\n\r\n",
		strings.TrimPrefix(ts.URL, "http://"))

	dir := t.TempDir()
	reqFile := filepath.Join(dir, "request.txt")
	if err := os.WriteFile(reqFile, []byte(rawRequest), 0o600); err != nil {
		t.Fatalf("write request file: %v", err)
	}

	payloadsDir := setupPayloadsDir(t)

	// Override package-level vars for the test
	folder = payloadsDir
	nobanner = true
	technique = []string{"verbs"}

	loadFlagsFromRequestFile(reqFile, true, true, []string{"verbs"}, false)

	mu.Lock()
	defer mu.Unlock()

	if len(capturedURIs) == 0 {
		t.Fatal("expected requests to be sent from request file, got 0")
	}

	// The first request (default) should contain the full query string
	foundQueryString := false
	for _, uri := range capturedURIs {
		if strings.Contains(uri, "action=get_config_data") && strings.Contains(uri, "user_id=239501342") {
			foundQueryString = true
			break
		}
	}
	if !foundQueryString {
		t.Errorf("query string was stripped from request file URL. Captured URIs: %v", capturedURIs)
	}
}

func TestRequestFileParsesPostWithBody(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var capturedMethods []string
	var capturedURIs []string
	var capturedHeaders []http.Header

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedMethods = append(capturedMethods, r.Method)
		capturedURIs = append(capturedURIs, r.URL.RequestURI())
		capturedHeaders = append(capturedHeaders, r.Header.Clone())
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	}))
	defer ts.Close()

	// Simulate a Burp-style POST request with HTTP/2 and body
	rawRequest := fmt.Sprintf("POST /api/v1/data HTTP/2\r\nHost: %s\r\nContent-Type: application/json\r\nX-Custom: test-value\r\n\r\n{\"key\":\"value\"}",
		strings.TrimPrefix(ts.URL, "http://"))

	dir := t.TempDir()
	reqFile := filepath.Join(dir, "request.txt")
	if err := os.WriteFile(reqFile, []byte(rawRequest), 0o600); err != nil {
		t.Fatalf("write request file: %v", err)
	}

	payloadsDir := setupPayloadsDir(t)
	folder = payloadsDir
	nobanner = true

	loadFlagsFromRequestFile(reqFile, true, true, []string{"verbs"}, false)

	mu.Lock()
	defer mu.Unlock()

	if len(capturedMethods) == 0 {
		t.Fatal("expected requests from request file, got 0")
	}

	// Default request should use the original POST method
	foundPost := false
	for _, m := range capturedMethods {
		if m == "POST" {
			foundPost = true
			break
		}
	}
	if !foundPost {
		t.Errorf("expected POST method to be used, got methods: %v", capturedMethods)
	}

	// URI should be correctly parsed
	foundURI := false
	for _, uri := range capturedURIs {
		if strings.Contains(uri, "/api/v1/data") {
			foundURI = true
			break
		}
	}
	if !foundURI {
		t.Errorf("expected /api/v1/data in URIs, got: %v", capturedURIs)
	}

	// Custom headers should be extracted
	foundCustomHeader := false
	for _, h := range capturedHeaders {
		if h.Get("X-Custom") == "test-value" {
			foundCustomHeader = true
			break
		}
	}
	if !foundCustomHeader {
		t.Errorf("expected X-Custom header, not found in captured headers")
	}
}

func TestRequestFileHandlesHTTP20(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var capturedURIs []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedURIs = append(capturedURIs, r.URL.RequestURI())
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	}))
	defer ts.Close()

	// Test with "HTTP/2.0" (another common Burp format)
	rawRequest := fmt.Sprintf("GET /admin/panel HTTP/2.0\r\nHost: %s\r\n\r\n",
		strings.TrimPrefix(ts.URL, "http://"))

	dir := t.TempDir()
	reqFile := filepath.Join(dir, "request.txt")
	if err := os.WriteFile(reqFile, []byte(rawRequest), 0o600); err != nil {
		t.Fatalf("write request file: %v", err)
	}

	payloadsDir := setupPayloadsDir(t)
	folder = payloadsDir
	nobanner = true

	loadFlagsFromRequestFile(reqFile, true, true, []string{"verbs"}, false)

	mu.Lock()
	defer mu.Unlock()

	if len(capturedURIs) == 0 {
		t.Fatal("expected requests from HTTP/2.0 request file, got 0")
	}

	foundURI := false
	for _, uri := range capturedURIs {
		if strings.Contains(uri, "/admin/panel") {
			foundURI = true
			break
		}
	}
	if !foundURI {
		t.Errorf("expected /admin/panel in URIs, got: %v", capturedURIs)
	}
}

func TestPayloadPositionsSendsRequests(t *testing.T) {
	resetTestState()

	ts, getRequests := testServer(t, nil)
	defer ts.Close()

	payloadsDir := setupPayloadsDir(t)

	opts := RequestOptions{
		uri:              ts.URL + "/100/admin",
		headers:          []header{{"User-Agent", "test"}},
		method:           "GET",
		proxy:            &url.URL{},
		folder:           payloadsDir,
		timeout:          5000,
		rateLimit:        false,
		redirect:         false,
		verbose:          true,
		payloadPositions: []string{"100"},
		uriTemplate:      ts.URL + "/" + payloadPlaceholder(0) + "/admin",
	}

	requestPayloadPositions(opts)

	reqs := getRequests()
	if len(reqs) == 0 {
		t.Fatal("expected payload position requests to be sent, got 0")
	}

	// Check that some requests replace "100" with payloads
	foundModified := false
	for _, r := range reqs {
		path := r.URL.Path
		if !strings.Contains(path, "/100/") && strings.Contains(path, "admin") {
			foundModified = true
			break
		}
	}
	if !foundModified {
		paths := make([]string, 0, len(reqs))
		for _, r := range reqs {
			paths = append(paths, r.URL.RequestURI())
		}
		t.Errorf("expected modified position paths, got: %v", paths)
	}
}

func TestUnicodeEncodingSendsEncodedPaths(t *testing.T) {
	resetTestState()

	ts, getRequests := testServer(t, nil)
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestUnicodeEncoding(opts)

	reqs := getRequests()
	if len(reqs) == 0 {
		t.Fatal("expected unicode encoding requests to be sent, got 0")
	}

	// Should have multiple requests: overlong slash replacements + per-char encoding
	if len(reqs) < 5 {
		t.Errorf("expected at least 5 unicode encoding requests, got %d", len(reqs))
	}
}

func TestVerbTamperingPreservesQueryString(t *testing.T) {
	resetTestState()

	ts, getRequests := testServer(t, nil)
	defer ts.Close()

	payloadsDir := setupPayloadsDir(t)

	opts := RequestOptions{
		uri:       ts.URL + "/admin?token=abc123&role=user",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		folder:    payloadsDir,
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestMethods(opts)

	reqs := getRequests()
	if len(reqs) == 0 {
		t.Fatal("expected requests, got 0")
	}

	// All requests should preserve the query string
	for _, r := range reqs {
		rawURI := r.URL.RequestURI()
		if !strings.Contains(rawURI, "token=abc123") || !strings.Contains(rawURI, "role=user") {
			t.Errorf("query string lost in verb tampering request: %s", rawURI)
		}
	}
}

func TestHeadersIncludesHostVariations(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var capturedHosts []string

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedHosts = append(capturedHosts, r.Host)
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	dir := setupPayloadsDir(t)

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		folder:    dir,
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestHeaders(opts)

	mu.Lock()
	defer mu.Unlock()

	// Should include Host variations (port suffixes, trailing dot, case switching)
	foundPortVariation := false
	foundTrailingDot := false
	for _, h := range capturedHosts {
		if strings.Contains(h, ":80") || strings.Contains(h, ":443") || strings.Contains(h, ":8080") {
			foundPortVariation = true
		}
		if strings.HasSuffix(h, ".") {
			foundTrailingDot = true
		}
	}

	if !foundPortVariation {
		t.Error("expected Host header port variations (e.g., :80, :443) to be sent")
	}
	if !foundTrailingDot {
		t.Error("expected Host header trailing dot variation to be sent")
	}
}

func TestHopByHopSendsRequests(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var capturedConnHeaders []string
	var capturedTargetHeaders []string

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedConnHeaders = append(capturedConnHeaders, r.Header.Get("Connection"))
		// Check that the target header is also sent with a bypass value
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			capturedTargetHeaders = append(capturedTargetHeaders, "X-Forwarded-For: "+xff)
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			capturedTargetHeaders = append(capturedTargetHeaders, "X-Real-IP: "+xri)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestHopByHop(opts)

	mu.Lock()
	defer mu.Unlock()

	if len(capturedConnHeaders) == 0 {
		t.Fatal("expected hop-by-hop requests to be sent, got 0")
	}

	// Connection header should list a security-relevant header for hop-by-hop stripping
	foundHopByHop := false
	for _, h := range capturedConnHeaders {
		if strings.Contains(h, "X-Forwarded-For") || strings.Contains(h, "X-Real-IP") || strings.Contains(h, "CF-Connecting-IP") {
			foundHopByHop = true
			break
		}
	}
	if !foundHopByHop {
		t.Error("expected at least one Connection header containing a security-relevant header name")
	}

	// The target header should also be sent with a bypass value
	if len(capturedTargetHeaders) == 0 {
		t.Error("expected hop-by-hop to also send the target header with a bypass value")
	}
}

func TestMethodOverrideQuerySendsRequests(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var capturedMethods []string
	var capturedURIs []string

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedMethods = append(capturedMethods, r.Method)
		capturedURIs = append(capturedURIs, r.URL.RequestURI())
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestMethodOverrideQuery(opts)

	mu.Lock()
	defer mu.Unlock()

	if len(capturedMethods) == 0 {
		t.Fatal("expected method override requests to be sent, got 0")
	}

	// All requests should use POST method
	for _, m := range capturedMethods {
		if m != "POST" {
			t.Errorf("expected POST method for _method override, got %s", m)
		}
	}

	// Check that _method query parameter is present
	found_method := false
	for _, uri := range capturedURIs {
		if strings.Contains(uri, "_method=") {
			found_method = true
			break
		}
	}
	if !found_method {
		t.Error("expected _method query parameter in at least one request")
	}
}

func TestNewTechniquesDoNotCrashOnRateLimit(t *testing.T) {
	resetTestState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "Rate limited")
	}))
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: true,
		redirect:  false,
		verbose:   true,
	}

	// None of these should crash
	requestHopByHop(opts)
	requestMethodOverrideQuery(opts)
}

func TestMethodOverrideHeadersSendsOverrideHeaders(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var captured []string

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		for _, name := range []string{"X-HTTP-Method-Override", "X-HTTP-Method", "X-Method-Override"} {
			if v := r.Header.Get(name); v != "" {
				captured = append(captured, name+": "+v)
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestMethodOverrideHeaders(opts)

	mu.Lock()
	defer mu.Unlock()
	if len(captured) == 0 {
		t.Fatal("expected method override headers to be sent")
	}
}

func TestMethodOverrideBodySendsBody(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var bodies []string
	var contentTypes []string

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		contentTypes = append(contentTypes, r.Header.Get("Content-Type"))
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestMethodOverrideBody(opts)

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("expected method override body requests to be sent")
	}
	foundForm := false
	foundJSON := false
	for i, body := range bodies {
		if strings.Contains(body, "_method=") && strings.Contains(contentTypes[i], "application/x-www-form-urlencoded") {
			foundForm = true
		}
		if strings.Contains(body, `"_method"`) && strings.Contains(contentTypes[i], "application/json") {
			foundJSON = true
		}
	}
	if !foundForm {
		t.Error("expected form _method body to be sent")
	}
	if !foundJSON {
		t.Error("expected json _method body to be sent")
	}
}

func TestRawDuplicatesSendsDuplicateHeaders(t *testing.T) {
	resetTestState()
	rawHTTP = true
	defer func() { rawHTTP = false }()

	var mu sync.Mutex
	var duplicateSeen bool

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if len(r.Header.Values("X-Forwarded-For")) > 1 || len(r.Header.Values("X-Original-URL")) > 1 {
			duplicateSeen = true
		}
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
		rawHTTP:   true,
	}

	requestRawDuplicates(opts)

	mu.Lock()
	defer mu.Unlock()
	if !duplicateSeen {
		t.Error("expected raw duplicates technique to send duplicate headers")
	}
}

func TestBuildCurlCommandIncludesBodyAndHeaders(t *testing.T) {
	cmd := buildCurlCommand("POST", "https://example.com/admin", []header{{"X-Test", "1"}}, "_method=DELETE", true, &url.URL{Scheme: "http", Host: "127.0.0.1:8080"})
	if !strings.Contains(cmd, "-X 'POST'") {
		t.Fatalf("expected curl command to include method, got %s", cmd)
	}
	if !strings.Contains(cmd, "--data '_method=DELETE'") {
		t.Fatalf("expected curl command to include body, got %s", cmd)
	}
	if !strings.Contains(cmd, "-H 'X-Test: 1'") {
		t.Fatalf("expected curl command to include header, got %s", cmd)
	}
}

func TestScoreResultRewardsBetterSignals(t *testing.T) {
	resetTestState()
	setDefaultSc(403)
	setDefaultRespCl(100)
	setTechniqueBaseline("headers", ResponseInfo{
		statusCode:    403,
		contentLength: 100,
		bodyHash:      "aaa",
		contentType:   "text/plain",
	})

	result := Result{
		line:          "X-Original-URL: /admin",
		statusCode:    200,
		contentLength: 500,
		bodyHash:      "bbb",
		contentType:   "text/html",
		technique:     "headers",
	}
	score := scoreResult(result)
	if score < 70 {
		t.Fatalf("expected strong candidate score, got %d", score)
	}
	if classifyLikelihood(score) != "high" {
		t.Fatalf("expected high likelihood, got %s", classifyLikelihood(score))
	}
}

func TestScoreResultKeepsStrong400AsVariation(t *testing.T) {
	resetTestState()
	setDefaultSc(403)
	setDefaultRespCl(520)
	setCalibTolerance(50)
	setTechniqueBaseline("midpaths", ResponseInfo{
		statusCode:    403,
		contentLength: 520,
		bodyHash:      "aaa",
		contentType:   "text/html",
	})

	result := Result{
		line:          "/..%00/web.config",
		statusCode:    400,
		contentLength: 122,
		bodyHash:      "bbb",
		contentType:   "text/html",
		technique:     "midpaths",
	}

	score := scoreResult(result)
	if score < 25 {
		t.Fatalf("expected a strong 400 variation to keep a non-trivial score, got %d", score)
	}
	if score >= 55 {
		t.Fatalf("expected a strong 400 variation not to look like likely bypass, got %d", score)
	}
}

func TestScoreResultRewardsSameStatusBodyDelta(t *testing.T) {
	resetTestState()
	setGlobalBaseline(ResponseInfo{
		statusCode:    403,
		contentLength: 520,
		bodyHash:      "aaa",
		contentType:   "text/html",
		server:        "nginx",
	})
	setTechniqueBaseline("headers-ip", globalBaseline())

	result := Result{
		line:          "X-Forwarded-For: 127.0.0.1",
		statusCode:    403,
		contentLength: 1911,
		bodyHash:      "bbb",
		contentType:   "application/json",
		server:        "envoy",
		technique:     "headers-ip",
	}

	score := scoreResult(result)
	if score < 55 {
		t.Fatalf("expected strong same-status anomaly to reach medium/high territory, got %d", score)
	}
}

func TestScoreReasonFlagsRedirectAnomaly(t *testing.T) {
	resetTestState()
	setGlobalBaseline(ResponseInfo{
		statusCode:    403,
		contentLength: 520,
		bodyHash:      "aaa",
		contentType:   "text/html",
	})
	setTechniqueBaseline("absolute-uri", globalBaseline())

	result := Result{
		line:          "request-target: https://example.com/admin",
		statusCode:    302,
		contentLength: 30,
		bodyHash:      "bbb",
		location:      "/dashboard",
		contentType:   "text/html",
		technique:     "absolute-uri",
	}

	reason := scoreReason(result)
	if !strings.Contains(reason, "redirect anomaly") {
		t.Fatalf("expected redirect anomaly in score reason, got %q", reason)
	}

	score := scoreResult(result)
	if score < 55 {
		t.Fatalf("expected redirect anomaly to produce a meaningful score, got %d", score)
	}
}

func TestScoreResultPenalizesSameStatusEmptyBody(t *testing.T) {
	resetTestState()
	setGlobalBaseline(ResponseInfo{
		statusCode:    403,
		contentLength: 118,
		bodyHash:      "aaa",
		contentType:   "text/html",
	})
	setTechniqueBaseline("verb-tampering", globalBaseline())

	result := Result{
		line:          "HEAD",
		statusCode:    403,
		contentLength: 0,
		bodyHash:      "bbb",
		contentType:   "",
		technique:     "verb-tampering",
	}

	score := scoreResult(result)
	if score >= 25 {
		t.Fatalf("expected same-status empty-body response to stay low-score, got %d", score)
	}
}

func TestScoreResultPenalizesAccessControlRedirects(t *testing.T) {
	resetTestState()
	setGlobalBaseline(ResponseInfo{
		statusCode:    403,
		contentLength: 118,
		bodyHash:      "aaa",
		contentType:   "text/html",
	})
	setTechniqueBaseline("endpaths", globalBaseline())

	result := Result{
		line:          "https://example.com/admin/.",
		statusCode:    302,
		contentLength: 0,
		bodyHash:      "",
		location:      "/403",
		contentType:   "",
		technique:     "endpaths",
	}

	if strings.Contains(scoreReason(result), "redirect anomaly") {
		t.Fatalf("did not expect access-control redirect to be tagged as anomaly")
	}

	score := scoreResult(result)
	if score >= 25 {
		t.Fatalf("expected access-control redirect to stay low-score, got %d", score)
	}
	if !strings.Contains(scoreReason(result), "redirect to access control") {
		t.Fatalf("expected access-control redirect reason, got %q", scoreReason(result))
	}
}

func TestForwardedTrustSendsForwardedHeaders(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var sawForwarded bool
	var sawClientIP bool

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if r.Header.Get("Forwarded") != "" {
			sawForwarded = true
		}
		if r.Header.Get("Client-IP") != "" || r.Header.Get("X-Client-Ip") != "" {
			sawClientIP = true
		}
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestForwardedTrust(opts)

	mu.Lock()
	defer mu.Unlock()
	if !sawForwarded || !sawClientIP {
		t.Fatalf("expected forwarded trust technique to send structured forwarding headers")
	}
}

func TestSuffixTricksSendsSuffixVariants(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var capturedURIs []string

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedURIs = append(capturedURIs, r.URL.RequestURI())
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin/panel",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestSuffixTricks(opts)

	mu.Lock()
	defer mu.Unlock()
	if len(capturedURIs) == 0 {
		t.Fatal("expected suffix trick requests to be sent")
	}
	foundSuffix := false
	foundQuery := false
	for _, uri := range capturedURIs {
		if strings.Contains(uri, ".json") || strings.Contains(uri, ";index.html") {
			foundSuffix = true
		}
		if strings.Contains(uri, "download=1") || strings.Contains(uri, "format=json") {
			foundQuery = true
		}
	}
	if !foundSuffix || !foundQuery {
		t.Fatalf("expected suffix and query variants, got %v", capturedURIs)
	}
}

func TestHostOverrideSendsModernHostHeaders(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var sawOverride bool
	var sawForwardedHost bool
	var sawHostMutation bool

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if r.Header.Get("X-HTTP-Host-Override") != "" || r.Header.Get("X-Host") != "" {
			sawOverride = true
		}
		if r.Header.Get("X-Forwarded-Host") != "" {
			sawForwardedHost = true
		}
		if strings.HasSuffix(r.Host, ".") || r.Host == strings.ToUpper(strings.TrimSuffix(r.Host, ".")) {
			sawHostMutation = true
		}
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestHostOverride(opts)

	mu.Lock()
	defer mu.Unlock()
	if !sawOverride || !sawForwardedHost || !sawHostMutation {
		t.Fatalf("expected host override technique to send override, forwarded-host, and host mutation variants")
	}
}

func TestProtoConfusionSendsProtoHeaders(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var sawProto bool
	var sawPort bool

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if r.Header.Get("X-Forwarded-Proto") != "" || r.Header.Get("X-Forwarded-Protocol") != "" {
			sawProto = true
		}
		if r.Header.Get("X-Forwarded-Port") != "" {
			sawPort = true
		}
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestProtoConfusion(opts)

	mu.Lock()
	defer mu.Unlock()
	if !sawProto || !sawPort {
		t.Fatalf("expected proto confusion technique to send proto and port trust headers")
	}
}

func TestIPEncodingHeadersSendsEncodedLocalhostVariants(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var sawIntegerIP bool
	var sawIPv6 bool

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		for _, value := range []string{
			r.Header.Get("X-Forwarded-For"),
			r.Header.Get("Client-IP"),
			r.Header.Get("True-Client-IP"),
			r.Header.Get("X-Real-Ip"),
			r.Header.Get("Cf-Connecting-Ip"),
		} {
			if value == "2130706433" {
				sawIntegerIP = true
			}
			if value == "[::1]" || value == "::ffff:127.0.0.1" {
				sawIPv6 = true
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestIPEncodingHeaders(opts)

	mu.Lock()
	defer mu.Unlock()
	if !sawIntegerIP || !sawIPv6 {
		t.Fatalf("expected ip encoding technique to send encoded localhost variants")
	}
}

func TestRawAuthoritySendsDuplicateHostHeaders(t *testing.T) {
	resetTestState()

	var mu sync.Mutex
	var authorityManipulated bool

	ts, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if len(r.Header.Values("X-Forwarded-Host")) > 1 || len(r.Header.Values("Forwarded")) > 1 || r.Host == "localhost" {
			authorityManipulated = true
		}
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Forbidden")
	})
	defer ts.Close()

	opts := RequestOptions{
		uri:       ts.URL + "/admin",
		headers:   []header{{"User-Agent", "test"}},
		method:    "GET",
		proxy:     &url.URL{},
		timeout:   5000,
		rateLimit: false,
		redirect:  false,
		verbose:   true,
	}

	requestRawAuthority(opts)

	mu.Lock()
	defer mu.Unlock()
	if !authorityManipulated {
		t.Fatalf("expected raw authority technique to manipulate host authority")
	}
}

func TestCrossTechniqueDedupCollapsesSimilarFamilies(t *testing.T) {
	resetTestState()
	setVerbose(false)
	setDefaultSc(403)
	setDefaultRespCl(520)
	setCalibTolerance(50)
	setTechniqueBaseline("headers", ResponseInfo{
		statusCode:    403,
		contentLength: 520,
		bodyHash:      "aaa",
		contentType:   "text/html",
	})
	setTechniqueBaseline("header-confusion", ResponseInfo{
		statusCode:    403,
		contentLength: 520,
		bodyHash:      "aaa",
		contentType:   "text/html",
	})

	first := Result{
		line:          "X-Original-URL -> path",
		statusCode:    400,
		contentLength: 122,
		bodyHash:      "bbb",
		contentType:   "text/html",
	}
	second := Result{
		line:          "X-Rewrite-URL -> path",
		statusCode:    400,
		contentLength: 122,
		bodyHash:      "bbb",
		contentType:   "text/html",
	}

	printResponse(first, "headers")
	printResponse(second, "header-confusion")

	if suppressedCrossTechniqueFamilies["header-confusion"] == 0 {
		t.Fatalf("expected similar cross-technique result to be collapsed")
	}
}
