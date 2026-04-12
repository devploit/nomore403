package cmd

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

// ErrRateLimited is returned when the server responds with HTTP 429.
var ErrRateLimited = fmt.Errorf("rate limited (HTTP 429)")

// clientCacheKey identifies a unique HTTP client configuration.
type clientCacheKey struct {
	proxy    string
	timeout  int
	redirect bool
}

var clientCache sync.Map

// getClient returns a cached HTTP client for the given configuration,
// creating one if needed. This enables connection pooling across requests.
func getClient(proxy *url.URL, timeout int, redirect bool) *http.Client {
	proxyStr := ""
	if proxy != nil {
		proxyStr = proxy.String()
	}
	key := clientCacheKey{proxyStr, timeout, redirect}

	if v, ok := clientCache.Load(key); ok {
		return v.(*http.Client)
	}

	timeoutDuration := time.Duration(timeout) * time.Millisecond
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxy),
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DialContext: (&net.Dialer{
			Timeout:   timeoutDuration,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeoutDuration,
		ResponseHeaderTimeout: timeoutDuration,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeoutDuration,
	}

	if !redirect {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	clientCache.Store(key, client)
	return client
}

// parseFile reads a file given its filename and returns a list containing each of its lines.
func parseFile(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func(file *os.File) {
		if err := file.Close(); err != nil {
			log.Printf("Error closing file: %v", err)
		}
	}(file)

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

// header represents an HTTP header.
type header struct {
	key   string
	value string
}

// requestWithRetry makes an HTTP request with retry logic and exponential backoff.
// It retries up to maxRetries times on transient errors (timeouts, connection errors).
// On HTTP 429, it retries with backoff if rateLimit is false; returns ErrRateLimited if rateLimit is true.
func requestWithRetry(method, uri string, headers []header, proxy *url.URL, rateLimit bool, timeout int, redirect bool) (ResponseInfo, error) {
	return requestWithRetryBody(method, uri, headers, "", proxy, rateLimit, timeout, redirect)
}

func requestWithRetryBody(method, uri string, headers []header, body string, proxy *url.URL, rateLimit bool, timeout int, redirect bool) (ResponseInfo, error) {
	maxRetries := retryCount
	if maxRetries < 0 {
		maxRetries = 0
	}
	backoffMs := retryBackoffMs
	if backoffMs <= 0 {
		backoffMs = 500
	}
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Duration(backoffMs) * time.Millisecond
			time.Sleep(backoff)
		}

		resp, err := requestBody(method, uri, headers, body, proxy, timeout, redirect)
		if err == nil {
			if resp.statusCode == 429 {
				if rateLimit {
					return resp, ErrRateLimited
				}
				lastErr = fmt.Errorf("HTTP 429 rate limited on attempt %d", attempt+1)
				continue
			}
			return resp, nil
		}

		lastErr = err
		if !isTransientError(err) {
			return ResponseInfo{}, err
		}
	}

	return ResponseInfo{}, fmt.Errorf("request failed after %d attempts: %w", maxRetries+1, lastErr)
}

// isTransientError returns true for errors that are likely transient and worth retrying.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	transientPatterns := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"EOF",
		"temporary failure",
		"no such host", // DNS can be transient
	}
	for _, pattern := range transientPatterns {
		if strings.Contains(strings.ToLower(errStr), strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// request makes a single HTTP request using headers `headers` and proxy `proxy`.
func request(method, uri string, headers []header, proxy *url.URL, timeout int, redirect bool) (ResponseInfo, error) {
	return requestBody(method, uri, headers, "", proxy, timeout, redirect)
}

func requestBody(method, uri string, headers []header, body string, proxy *url.URL, timeout int, redirect bool) (ResponseInfo, error) {
	if method == "" {
		method = "GET"
	}

	if proxy == nil || len(proxy.Host) == 0 {
		proxy = nil
	}

	// net/http and url.Parse do not accept legacy IIS-style %uXXXX escapes.
	// Route those requests through the raw client so the request target is sent as-is.
	if strings.Contains(strings.ToLower(uri), "%u") && proxy == nil && !redirect {
		return rawRequest(method, uri, rawRequestTarget(uri), headers, body, timeout)
	}

	client := getClient(proxy, timeout, redirect)

	parsedURL, err := url.Parse(uri)
	if err != nil || parsedURL == nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		// Fallback for non-standard encoding (e.g., %u002f unicode escapes)
		// that url.Parse rejects. Extract scheme/host manually and preserve
		// the raw path so the server receives it as-is.
		parsedURL, err = parseRawURL(uri)
		if err != nil {
			return ResponseInfo{}, fmt.Errorf("invalid URL: %q", uri)
		}
	} else {
		parsedURL.RawPath = parsedURL.EscapedPath()
	}

	req, err := http.NewRequest(method, parsedURL.String(), strings.NewReader(body))
	if err != nil {
		return ResponseInfo{}, err
	}
	req.Host = parsedURL.Host
	req.URL = parsedURL
	req.Header = make(http.Header)

	for _, header := range headers {
		// Go's net/http ignores req.Header["Host"] — it uses req.Host instead.
		// Set req.Host directly so Host header variations are actually sent.
		if strings.EqualFold(header.key, "Host") {
			req.Host = header.value
		} else {
			req.Header.Add(header.key, header.value)
		}
	}
	if body != "" && req.Header.Get("Content-Length") == "" {
		req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	}

	res, err := client.Do(req)
	if err != nil {
		return ResponseInfo{}, err
	}
	defer func() {
		if cerr := res.Body.Close(); cerr != nil {
			log.Printf("[!] Error closing response body: %v", cerr)
		}
	}()

	bodySize, bodyHash, _ := captureBodySignature(res.Body)
	return ResponseInfo{
		statusCode:    res.StatusCode,
		contentLength: bodySize,
		bodyHash:      bodyHash,
		location:      res.Header.Get("Location"),
		contentType:   res.Header.Get("Content-Type"),
		server:        res.Header.Get("Server"),
		via:           res.Header.Get("Via"),
		xCache:        res.Header.Get("X-Cache"),
		poweredBy:     res.Header.Get("X-Powered-By"),
		cfRay:         res.Header.Get("CF-Ray"),
	}, nil
}

// loadFlagsFromRequestFile parse an HTTP request and configure the necessary flags for an execution
func loadFlagsFromRequestFile(requestFile string, schema bool, verbose bool, techniques []string, redirect bool) {
	// Read the content of the request file
	content, err := os.ReadFile(requestFile)
	if err != nil {
		log.Printf("[!] Error reading request file: %v", err)
		return
	}

	temp := strings.Split(string(content), "\n")
	if len(temp) == 0 {
		log.Printf("[!] Request file is empty: %s", requestFile)
		return
	}

	// Down HTTP/2 to HTTP/1.1 (handles both "HTTP/2" and "HTTP/2.0")
	firstLine := temp[0]
	if strings.Contains(firstLine, "HTTP/2.0") {
		firstLine = strings.Replace(firstLine, "HTTP/2.0", "HTTP/1.1", 1)
	} else if strings.Contains(firstLine, "HTTP/2") {
		firstLine = strings.Replace(firstLine, "HTTP/2", "HTTP/1.1", 1)
	}
	content = []byte(strings.Join(append([]string{firstLine}, temp[1:]...), "\n"))

	reqReader := strings.NewReader(string(content))
	req, err := http.ReadRequest(bufio.NewReader(reqReader))
	if err != nil {
		log.Printf("[!] Error parsing request file: %v", err)
		return
	}

	if strings.HasPrefix(req.RequestURI, "http://") {
		parts := strings.SplitAfterN(req.RequestURI, "/", 4)
		if len(parts) >= 4 {
			req.RequestURI = "/" + parts[3]
		}
	}

	httpSchema := "https://"
	if schema {
		httpSchema = "http://"
	}

	uri := httpSchema + req.Host + req.RequestURI

	// Extract headers from the request
	var reqHeaders []string
	for k, v := range req.Header {
		reqHeaders = append(reqHeaders, k+": "+strings.Join(v, ""))
	}
	httpMethod := req.Method
	requester(uri, proxy, userAgent, reqHeaders, bypassIP, folder, httpMethod, verbose, techniques, nobanner, rateLimit, timeout, redirect, randomAgent)
}

// calibrationTolerance defines the acceptable variance in content-length between calibration samples.
const calibrationTolerance = 50

func runAutocalibrate(options RequestOptions) (int, int) {
	calibrationPaths := []string{"calibration_test_123456", "calib_nonexist_789xyz", "zz_calibrate_000"}
	var samples []int

	baseURI := options.uri
	if !strings.HasSuffix(baseURI, "/") {
		baseURI += "/"
	}

	var lastStatusCode int
	for _, path := range calibrationPaths {
		calibrationURI := baseURI + path
		resp, err := requestWithRetry("GET", calibrationURI, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
		if err != nil {
			log.Printf("[!] Error during calibration request (%s): %v\n", path, err)
			continue
		}
		lastStatusCode = resp.statusCode
		samples = append(samples, resp.contentLength)
	}

	if len(samples) == 0 {
		log.Printf("[!] All calibration requests failed, disabling auto-calibration filtering")
		return 0, 0
	}

	// Calculate average and max deviation
	sum := 0
	for _, s := range samples {
		sum += s
	}
	avgCl := sum / len(samples)

	maxDeviation := 0
	for _, s := range samples {
		dev := s - avgCl
		if dev < 0 {
			dev = -dev
		}
		if dev > maxDeviation {
			maxDeviation = dev
		}
	}

	// Use tolerance = max(calibrationTolerance, maxDeviation*2) to handle dynamic content
	tolerance := calibrationTolerance
	if maxDeviation*2 > tolerance {
		tolerance = maxDeviation * 2
	}

	// Fragment calibration: request URI#fragment to baseline fragment-stripped responses.
	// Since # is a fragment separator, the server receives the parent path instead of the
	// target path. This catches false positives from any payload that accidentally creates
	// a fragment URL (e.g., midpath "#" → domain.com/#path → requests domain.com/).
	parsedURI, parseErr := url.Parse(options.uri)
	if parseErr == nil && parsedURI.Path != "" && parsedURI.Path != "/" {
		// Build parent path: /api/admin → /api/
		parentPath := parsedURI.Path
		if strings.HasSuffix(parentPath, "/") {
			parentPath = parentPath[:len(parentPath)-1]
		}
		lastSlash := strings.LastIndex(parentPath, "/")
		if lastSlash >= 0 {
			parentPath = parentPath[:lastSlash+1]
		}
		fragmentURI := parsedURI.Scheme + "://" + parsedURI.Host + parentPath + "#calibration_fragment"
		fragResp, fragErr := requestWithRetry("GET", fragmentURI, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
		if fragErr == nil {
			setFragmentCl(fragResp.contentLength)
		}
	}

	fmt.Println(color.MagentaString("\n━━━━━━━━━━━━━━━ AUTO-CALIBRATION RESULTS ━━━━━━━━━━━━━━━"))
	fmt.Printf("[✔] Calibration samples: %d\n", len(samples))
	fmt.Printf("[✔] Status Code: %d\n", lastStatusCode)
	fmt.Printf("[✔] Avg Content Length: %d bytes (tolerance: ±%d)\n", avgCl, tolerance)
	if getFragmentCl() > 0 {
		fmt.Printf("[✔] Fragment baseline: %d bytes\n", getFragmentCl())
	}

	return avgCl, tolerance
}

// parseRawURL extracts scheme, host, and raw path from a URI without decoding
// percent-encoded sequences. This allows non-standard encodings like %u002f
// (IIS-style Unicode escapes) to be sent to the server as-is.
func parseRawURL(rawURI string) (*url.URL, error) {
	idx := strings.Index(rawURI, "://")
	if idx < 0 {
		return nil, fmt.Errorf("missing scheme")
	}
	scheme := rawURI[:idx]
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", scheme)
	}
	rest := rawURI[idx+3:]

	slashIdx := strings.Index(rest, "/")
	var host, rawPath string
	if slashIdx < 0 {
		host = rest
		rawPath = "/"
	} else {
		host = rest[:slashIdx]
		rawPath = rest[slashIdx:]
	}

	if host == "" {
		return nil, fmt.Errorf("missing host")
	}

	// Split raw path and query
	rawQuery := ""
	if qIdx := strings.Index(rawPath, "?"); qIdx >= 0 {
		rawQuery = rawPath[qIdx+1:]
		rawPath = rawPath[:qIdx]
	}

	return &url.URL{
		Scheme:   scheme,
		Host:     host,
		Opaque:   rawPath,
		RawQuery: rawQuery,
	}, nil
}

func rawRequestTarget(rawURI string) string {
	idx := strings.Index(rawURI, "://")
	if idx < 0 {
		return "/"
	}
	rest := rawURI[idx+3:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx < 0 {
		return "/"
	}
	target := rest[slashIdx:]
	if target == "" {
		return "/"
	}
	return target
}

func captureBodySignature(r io.Reader) (int, string, error) {
	hasher := fnv.New64a()
	buf := make([]byte, 4096)
	total := 0

	for {
		n, err := r.Read(buf)
		if n > 0 {
			total += n
			_, _ = hasher.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return total, "", err
		}
	}

	return total, fmt.Sprintf("%x", hasher.Sum64()), nil
}

func rawRequest(method, uri string, requestTarget string, headers []header, body string, timeout int) (ResponseInfo, error) {
	parsedURL, err := url.Parse(uri)
	if err != nil || parsedURL == nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		parsedURL, err = parseRawURL(uri)
		if err != nil {
			return ResponseInfo{}, err
		}
	}

	addr := parsedURL.Host
	if parsedURL.Port() == "" {
		port := "80"
		if parsedURL.Scheme == "https" {
			port = "443"
		}
		addr = net.JoinHostPort(parsedURL.Hostname(), port)
	}

	dialer := &net.Dialer{Timeout: time.Duration(timeout) * time.Millisecond}
	var conn net.Conn
	if parsedURL.Scheme == "https" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         parsedURL.Hostname(),
		})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return ResponseInfo{}, err
	}
	defer func() {
		_ = conn.Close()
	}()
	_ = conn.SetDeadline(time.Now().Add(time.Duration(timeout) * time.Millisecond))

	if requestTarget == "" {
		requestTarget = rawRequestTarget(uri)
	}

	hasHost := false
	hasConnection := false
	hasContentLength := false
	var builder strings.Builder
	builder.WriteString(method)
	builder.WriteString(" ")
	builder.WriteString(requestTarget)
	builder.WriteString(" HTTP/1.1\r\n")
	for _, h := range headers {
		if strings.EqualFold(h.key, "Host") {
			hasHost = true
		}
		if strings.EqualFold(h.key, "Connection") {
			hasConnection = true
		}
		if strings.EqualFold(h.key, "Content-Length") {
			hasContentLength = true
		}
		builder.WriteString(h.key)
		builder.WriteString(": ")
		builder.WriteString(h.value)
		builder.WriteString("\r\n")
	}
	if !hasHost {
		builder.WriteString("Host: ")
		builder.WriteString(parsedURL.Host)
		builder.WriteString("\r\n")
	}
	if !hasConnection {
		builder.WriteString("Connection: close\r\n")
	}
	if body != "" && !hasContentLength {
		builder.WriteString("Content-Length: ")
		builder.WriteString(strconv.Itoa(len(body)))
		builder.WriteString("\r\n")
	}
	builder.WriteString("\r\n")
	builder.WriteString(body)

	if _, err := io.WriteString(conn, builder.String()); err != nil {
		return ResponseInfo{}, err
	}

	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return ResponseInfo{}, err
	}
	defer func() {
		_ = res.Body.Close()
	}()

	bodySize, bodyHash, _ := captureBodySignature(res.Body)
	return ResponseInfo{
		statusCode:    res.StatusCode,
		contentLength: bodySize,
		bodyHash:      bodyHash,
		location:      res.Header.Get("Location"),
		contentType:   res.Header.Get("Content-Type"),
		server:        res.Header.Get("Server"),
		via:           res.Header.Get("Via"),
		xCache:        res.Header.Get("X-Cache"),
		poweredBy:     res.Header.Get("X-Powered-By"),
		cfRay:         res.Header.Get("CF-Ray"),
	}, nil
}
