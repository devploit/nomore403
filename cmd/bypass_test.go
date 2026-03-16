package cmd

import (
	"fmt"
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
	setDefaultCl(1000)
	setCalibTolerance(50)

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
