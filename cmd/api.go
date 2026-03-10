package cmd

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
)

// ErrRateLimited is returned when the server responds with HTTP 429.
var ErrRateLimited = fmt.Errorf("rate limited (HTTP 429)")

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
func requestWithRetry(method, uri string, headers []header, proxy *url.URL, rateLimit bool, timeout int, redirect bool) (int, []byte, error) {
	const maxRetries = 2
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt)) * 500 * time.Millisecond
			time.Sleep(backoff)
		}

		statusCode, resp, err := request(method, uri, headers, proxy, timeout, redirect)
		if err == nil {
			// Handle rate limiting
			if statusCode == 429 {
				if rateLimit {
					return statusCode, resp, ErrRateLimited
				}
				lastErr = fmt.Errorf("HTTP 429 rate limited on attempt %d", attempt+1)
				continue
			}
			return statusCode, resp, nil
		}

		lastErr = err
		// Only retry on transient errors (timeouts, connection refused, etc.)
		if !isTransientError(err) {
			return 0, nil, err
		}
		if attempt < maxRetries {
			log.Printf("[!] Transient error (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
		}
	}

	return 0, nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries+1, lastErr)
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
func request(method, uri string, headers []header, proxy *url.URL, timeout int, redirect bool) (int, []byte, error) {
	if method == "" {
		method = "GET"
	}

	if proxy == nil || len(proxy.Host) == 0 {
		proxy = nil
	}

	timeoutDuration := time.Duration(timeout) * time.Millisecond
	customTransport := &http.Transport{
		Proxy: http.ProxyURL(proxy),
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DialContext: (&net.Dialer{
			Timeout:   timeoutDuration,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeoutDuration,
		ResponseHeaderTimeout: timeoutDuration,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: customTransport,
		Timeout:   timeoutDuration,
	}

	if !redirect {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	parsedURL, err := url.Parse(uri)
	if err != nil || parsedURL == nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return 0, nil, fmt.Errorf("invalid URL: %q", uri)
	}

	parsedURL.RawPath = parsedURL.EscapedPath()

	req := &http.Request{
		Method: method,
		Host:   parsedURL.Host,
		URL:    parsedURL,
		Header: make(http.Header),
		Close:  true,
	}

	for _, header := range headers {
		req.Header.Add(header.key, header.value)
	}

	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() {
		if cerr := res.Body.Close(); cerr != nil {
			log.Printf("[!] Error closing response body: %v", cerr)
		}
	}()

	resp, err := httputil.DumpResponse(res, true)
	if err != nil {
		return 0, nil, err
	}

	return res.StatusCode, resp, nil
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

	// Down HTTP/2 to HTTP/1.1
	firstLine := strings.Replace(temp[0], "HTTP/2", "HTTP/1.1", 1)
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
		statusCode, response, err := requestWithRetry("GET", calibrationURI, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
		if err != nil {
			log.Printf("[!] Error during calibration request (%s): %v\n", path, err)
			continue
		}
		lastStatusCode = statusCode
		samples = append(samples, len(response))
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

	fmt.Println(color.MagentaString("\n━━━━━━━━━━━━━━━ AUTO-CALIBRATION RESULTS ━━━━━━━━━━━━━━━"))
	fmt.Printf("[✔] Calibration samples: %d\n", len(samples))
	fmt.Printf("[✔] Status Code: %d\n", lastStatusCode)
	fmt.Printf("[✔] Avg Content Length: %d bytes (tolerance: ±%d)\n", avgCl, tolerance)

	return avgCl, tolerance
}
