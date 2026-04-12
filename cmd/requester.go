package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/fatih/color"
	"github.com/zenthangplus/goccm"
)

type Result struct {
	line          string
	statusCode    int
	contentLength int
	defaultReq    bool
}

type RequestOptions struct {
	uri              string
	headers          []header
	method           string
	proxy            *url.URL
	userAgent        string
	redirect         bool
	folder           string
	bypassIP         string
	timeout          int
	rateLimit        bool
	techniques       []string
	verbose          bool
	reqHeaders       []string
	banner           bool
	autocalibrate    bool
	payloadPositions []string // extracted marked segments from URL template
	uriTemplate      string  // original URI with markers for payload injection
}

// Thread-safe globals using atomic operations.
var atomicVerbose int32
var atomicDefaultCl int64
var atomicCalibTolerance int64
var atomicDefaultSc int32  // default request status code
var atomicDefaultRespCl int64 // default request content-length (separate from calibration)

func getVerbose() bool       { return atomic.LoadInt32(&atomicVerbose) != 0 }
func logVerbose(v ...interface{}) {
	if getVerbose() {
		log.Println(v...)
	}
}
func setVerbose(v bool)      { var val int32; if v { val = 1 }; atomic.StoreInt32(&atomicVerbose, val) }
func getDefaultCl() int      { return int(atomic.LoadInt64(&atomicDefaultCl)) }
func setDefaultCl(v int)     { atomic.StoreInt64(&atomicDefaultCl, int64(v)) }
func getCalibTolerance() int { return int(atomic.LoadInt64(&atomicCalibTolerance)) }
func setCalibTolerance(v int) {
	atomic.StoreInt64(&atomicCalibTolerance, int64(v))
}
func getDefaultSc() int      { return int(atomic.LoadInt32(&atomicDefaultSc)) }
func setDefaultSc(v int)     { atomic.StoreInt32(&atomicDefaultSc, int32(v)) }
func getDefaultRespCl() int  { return int(atomic.LoadInt64(&atomicDefaultRespCl)) }
func setDefaultRespCl(v int) { atomic.StoreInt64(&atomicDefaultRespCl, int64(v)) }

// isCalibrationMatch returns true if the content length is within tolerance of the calibrated default.
func isCalibrationMatch(contentLength int) bool {
	cl := getDefaultCl()
	if cl == 0 {
		return false
	}
	diff := contentLength - cl
	if diff < 0 {
		diff = -diff
	}
	return diff <= getCalibTolerance()
}

// isInteresting returns true if the result differs meaningfully from the default response.
// A result is interesting if:
// - The status code differs from the default request's status code, OR
// - The content length differs significantly from the default request's content length
func isInteresting(result Result) bool {
	defSc := getDefaultSc()
	defCl := getDefaultRespCl()

	// If we have no baseline yet, show everything
	if defSc == 0 && defCl == 0 {
		return true
	}

	// Different status code = always interesting
	if result.statusCode != defSc {
		return true
	}

	// Same status code: only interesting if content length differs beyond tolerance
	if defCl > 0 {
		diff := result.contentLength - defCl
		if diff < 0 {
			diff = -diff
		}
		tolerance := getCalibTolerance()
		if tolerance == 0 {
			tolerance = calibrationTolerance
		}
		if diff > tolerance {
			return true
		}
	}

	return false
}

var uniqueResults = make(map[string]bool)
var uniqueResultsByTechnique = make(map[string]map[string]bool)
var verbTamperingResults = make(map[string]int)

// smartDedupMax is the maximum number of results to show per technique per status+CL group
// in non-verbose mode. Additional results are counted and summarized.
const smartDedupMax = 3

// techSignatureCounts tracks how many results per technique+status+CL we've seen.
var techSignatureCounts = make(map[string]int)
var techSignatureCountsMutex = &sync.Mutex{}

var (
	printMutex               = &sync.Mutex{}
	uniqueResultsMutex       = &sync.Mutex{}
	uniqueResultsByTechMutex = &sync.Mutex{}
)

// printResponse prints the results of HTTP requests in a tabular format with colored output based on the status codes.
func printResponse(result Result, technique string) {
	printMutex.Lock()
	defer printMutex.Unlock()

	// If verbose mode is enabled, directly print the result
	if getVerbose() || technique == "http-versions" {
		printResult(result)
		writeResultToOutput(result, technique)
		return
	}

	// In non-verbose (smart) mode: only show results that differ from the default response.
	// Default request itself is always shown.
	if !result.defaultReq && !isInteresting(result) {
		return
	}

	// Filter by specific status codes if filtering is enabled
	if len(statusCodes) > 0 {
		statusMatch := false
		for _, code := range statusCodes {
			if strconv.Itoa(result.statusCode) == code {
				statusMatch = true
				break
			}
		}
		if !statusMatch {
			return
		}
	}

	// Check for unique global output if uniqueOutput is enabled
	if uniqueOutput {
		globalKey := fmt.Sprintf("%d-%d", result.statusCode, result.contentLength)

		uniqueResultsMutex.Lock()
		if uniqueResults[globalKey] {
			uniqueResultsMutex.Unlock()
			return
		}
		uniqueResults[globalKey] = true
		uniqueResultsMutex.Unlock()
	}

	// Additional deduplication by technique
	uniqueResultsByTechMutex.Lock()
	if _, exists := uniqueResultsByTechnique[technique]; !exists {
		uniqueResultsByTechnique[technique] = make(map[string]bool)
	}
	techniqueKey := fmt.Sprintf("%d-%s", result.contentLength, result.line)
	if uniqueResultsByTechnique[technique][techniqueKey] {
		uniqueResultsByTechMutex.Unlock()
		return
	}
	uniqueResultsByTechnique[technique][techniqueKey] = true
	uniqueResultsByTechMutex.Unlock()

	// Smart dedup: limit results per technique+status+CL group.
	// Show up to smartDedupMax examples, then suppress and track count for summary.
	if !result.defaultReq {
		sigKey := fmt.Sprintf("%s:%d:%d", technique, result.statusCode, result.contentLength)
		techSignatureCountsMutex.Lock()
		techSignatureCounts[sigKey]++
		count := techSignatureCounts[sigKey]
		techSignatureCountsMutex.Unlock()

		if count > smartDedupMax {
			return // suppressed, will be summarized by printSuppressedSummary
		}
	}

	// Print the result after all filters are applied
	printResult(result)
	writeResultToOutput(result, technique)
}

// printSuppressedSummary prints a summary of suppressed results for a technique.
// Should be called after a technique finishes.
func printSuppressedSummary(technique string) {
	if getVerbose() {
		return
	}

	printMutex.Lock()
	defer printMutex.Unlock()

	techSignatureCountsMutex.Lock()
	defer techSignatureCountsMutex.Unlock()

	prefix := technique + ":"
	for key, count := range techSignatureCounts {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		suppressed := count - smartDedupMax
		if suppressed > 0 {
			// Extract status and CL from key "technique:status:cl"
			parts := strings.SplitN(key, ":", 3)
			if len(parts) == 3 {
				fmt.Printf("  ... and %d more with %s/%s bytes (use -v to see all)\n",
					suppressed, parts[1], parts[2])
			}
		}
	}
}

// colorizeContentLength returns the content-length string colored based on
// how much it differs from the default response:
//   - Much larger (>2x) = green (likely real content / bypass)
//   - Larger = cyan
//   - Similar = dim/blue (default)
//   - Smaller = yellow
//   - Much smaller (<50%) = red
func colorizeContentLength(cl int) string {
	clStr := strconv.Itoa(cl) + " bytes"
	defCl := getDefaultRespCl()

	if defCl == 0 || cl == 0 {
		return color.BlueString(clStr)
	}

	ratio := float64(cl) / float64(defCl)
	switch {
	case ratio > 2.0:
		return color.GreenString(clStr)
	case ratio > 1.2:
		return color.CyanString(clStr)
	case ratio < 0.5:
		return color.RedString(clStr)
	case ratio < 0.8:
		return color.YellowString(clStr)
	default:
		return color.BlueString(clStr)
	}
}

// printResult prints the result of an HTTP request in a tabular format with colored output based on the status codes.
// Caller must hold printMutex.
func printResult(result Result) {
	var code string

	// Assign colors to HTTP status codes based on their range
	switch result.statusCode {
	case 0:
		return
	case 200, 201, 202, 203, 204, 205, 206:
		code = color.GreenString(strconv.Itoa(result.statusCode))
	case 300, 301, 302, 303, 304, 307, 308:
		code = color.YellowString(strconv.Itoa(result.statusCode))
	case 400, 401, 402, 403, 404, 405, 406, 407, 408, 413, 429:
		code = color.RedString(strconv.Itoa(result.statusCode))
	case 500, 501, 502, 503, 504, 505, 511:
		code = color.MagentaString(strconv.Itoa(result.statusCode))
	default:
		code = strconv.Itoa(result.statusCode)
	}

	clStr := colorizeContentLength(result.contentLength)

	// Clear progress bar, print result, redraw progress bar
	clearActiveProgress()
	fmt.Printf("%s \t%20s %s\n", code, clStr, result.line)
	redrawActiveProgress()
}

// showInfo prints the configuration options used for the scan in a compact two-column layout.
func showInfo(options RequestOptions) {
	if nobanner {
		return
	}

	dim := color.New(color.Faint).SprintFunc()
	val := color.New(color.FgWhite, color.Bold).SprintFunc()

	fmt.Println(color.MagentaString("━━━━━━━━━━━━━━━━━━━━━━ NOMORE403 ━━━━━━━━━━━━━━━━━━━━━━━"))
	fmt.Printf("  %s %s\n", dim("Target:"), val(options.uri))

	// Row 1: Method + User-Agent
	fmt.Printf("  %s %s %s %s\n",
		dim("Method:"), val(options.method),
		dim("User-Agent:"), val(options.userAgent))

	// Row 2: Timeout + Delay
	fmt.Printf("  %s %s %s %s\n",
		dim("Timeout:"), val(fmt.Sprintf("%dms", options.timeout)),
		dim("Delay:"), val(fmt.Sprintf("%dms", delay)))

	// Row 3: Proxy + Bypass IP
	proxyStr := "-"
	if len(options.proxy.Host) != 0 {
		proxyStr = options.proxy.Host
	}
	ipStr := "-"
	if len(options.bypassIP) != 0 {
		ipStr = options.bypassIP
	}
	fmt.Printf("  %s %s %s %s\n",
		dim("Proxy:"), val(proxyStr),
		dim("Bypass IP:"), val(ipStr))

	// Row 4: Flags
	var flags []string
	if options.redirect {
		flags = append(flags, "redirects")
	}
	if options.rateLimit {
		flags = append(flags, "rate-limit")
	}
	if uniqueOutput {
		flags = append(flags, "unique")
	}
	if options.verbose {
		flags = append(flags, "verbose")
	}
	flagStr := "-"
	if len(flags) > 0 {
		flagStr = strings.Join(flags, ", ")
	}
	fmt.Printf("  %s %s\n", dim("Flags:"), val(flagStr))

	// Row 5: Headers
	if len(options.reqHeaders) > 0 && options.reqHeaders[0] != "" {
		hdrs := make([]string, 0)
		for _, h := range options.headers {
			if h.key != "User-Agent" {
				hdrs = append(hdrs, h.key+": "+h.value)
			}
		}
		if len(hdrs) > 0 {
			fmt.Printf("  %s %s\n", dim("Headers:"), val(strings.Join(hdrs, " | ")))
		}
	}

	// Row 6: Status filter
	if len(statusCodes) > 0 {
		fmt.Printf("  %s %s\n", dim("Status filter:"), val(strings.Join(statusCodes, ", ")))
	}

	// Row 7: Techniques
	fmt.Printf("  %s %s\n",
		dim("Techniques:"), val(strings.Join(options.techniques, ", ")))

	fmt.Printf("  %s %s\n", dim("Payloads:"), val(options.folder))
}

// generateCaseCombinations generates all combinations of uppercase and lowercase letters for a given string.
func generateCaseCombinations(s string) []string {
	if len(s) == 0 {
		return []string{""}
	}

	firstCharCombinations := []string{string(unicode.ToLower(rune(s[0]))), string(unicode.ToUpper(rune(s[0])))}
	subCombinations := generateCaseCombinations(s[1:])
	var combinations []string

	for _, char := range firstCharCombinations {
		for _, comb := range subCombinations {
			combinations = append(combinations, char+comb)
		}
	}

	return combinations
}

// filterOriginalMethod extract the original method from the list of combinations
func filterOriginalMethod(originalMethod string, combinations []string) []string {
	filtered := make([]string, 0, len(combinations))
	for _, combination := range combinations {
		if combination != originalMethod {
			filtered = append(filtered, combination)
		}
	}
	return filtered
}

// selectRandomCombinations selects up to n random combinations from a list of combinations.
func selectRandomCombinations(combinations []string, n int) []string {
	if len(combinations) <= n {
		return combinations
	}

	rand.Shuffle(len(combinations), func(i, j int) {
		combinations[i], combinations[j] = combinations[j], combinations[i]
	})

	return combinations[:n]
}

// requestDefault makes HTTP request to check the default response
func requestDefault(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━━━━ DEFAULT REQUEST ━━━━━━━━━━━━━━")

	statusCode, respSize, err := requestWithRetry(options.method, options.uri, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
	if err != nil {
		if errors.Is(err, ErrRateLimited) {
			log.Printf("[!] Rate limited on default request, aborting")
			return
		}
		log.Println(err)
	}

	// Capture the default response signature for smart filtering
	setDefaultSc(statusCode)
	setDefaultRespCl(respSize)
	if !options.autocalibrate || getDefaultCl() == 0 {
		setDefaultCl(respSize)
	}

	printResponse(Result{options.uri, statusCode, respSize, true}, "default")
}

// requestMethods makes HTTP requests using a list of methods from a file and prints the results.
func requestMethods(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━━━━ VERB TAMPERING ━━━━━━━━━━━━━━━")

	lines, err := parseFile(options.folder + "/httpmethods")
	if err != nil {
		log.Printf("[!] Skipping verb tampering: %v", err)
		return
	}
	if len(lines) == 0 {
		return
	}

	w := goccm.New(maxGoroutines)
	var verbTamperingResultsMutex = &sync.Mutex{}
	p := newProgress("verb-tampering", len(lines))

	for _, line := range lines {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			defer w.Done()
			defer p.done()
			statusCode, respSize, err := requestWithRetry(line, options.uri, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					log.Printf("[!] Rate limited, stopping verb tampering")
					return
				}
				logVerbose(err)
				return
			}

			contentLength := respSize

			if isCalibrationMatch(contentLength) {
				return
			}

			verbTamperingResultsMutex.Lock()
			verbTamperingResults[line] = contentLength
			verbTamperingResultsMutex.Unlock()

			result := Result{
				line:          line,
				statusCode:    statusCode,
				contentLength: contentLength,
				defaultReq:    false,
			}
			printResponse(result, "verb-tampering")
		}(line)
	}
	w.WaitAllDone()
	p.finish()
}

// requestMethodsCaseSwitching makes HTTP requests using a list of methods from a file and prints the results.
func requestMethodsCaseSwitching(options RequestOptions) {
	color.Cyan("\n━━━━━━━ VERB TAMPERING CASE SWITCHING ━━━━━━━━")

	lines, err := parseFile(options.folder + "/httpmethods")
	if err != nil {
		log.Printf("[!] Skipping verb case switching: %v", err)
		return
	}

	// Pre-build all work items to know the total for progress.
	type workItem struct {
		method              string
		originalContentLength int
	}
	var items []workItem
	for _, line := range lines {
		originalContentLength, exists := verbTamperingResults[line]
		if !exists {
			continue
		}
		methodCombinations := generateCaseCombinations(line)
		filteredCombinations := filterOriginalMethod(line, methodCombinations)
		selectedCombinations := selectRandomCombinations(filteredCombinations, 50)
		for _, method := range selectedCombinations {
			items = append(items, workItem{method, originalContentLength})
		}
	}

	if len(items) == 0 {
		return
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("verb-case-switching", len(items))

	for _, item := range items {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(item workItem) {
			defer w.Done()
			defer p.done()
			statusCode, respSize, err := requestWithRetry(item.method, options.uri, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			contentLength := respSize

			if contentLength == item.originalContentLength || isCalibrationMatch(contentLength) {
				return
			}

			result := Result{
				line:          item.method,
				statusCode:    statusCode,
				contentLength: contentLength,
				defaultReq:    false,
			}

			printResponse(result, "verb-tampering-case")
		}(item)
	}
	w.WaitAllDone()
	p.finish()
}

// requestHeaders makes HTTP requests using a list of headers from a file and prints the results.
func requestHeaders(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━━━━━━━ HEADERS ━━━━━━━━━━━━━━━━━━━")

	// Load headers from file
	lines, err := parseFile(options.folder + "/headers")
	if err != nil {
		log.Printf("[!] Skipping headers technique: %v", err)
		return
	}

	// Load IPs for bypassing or use provided bypass IP
	var ips []string
	if len(options.bypassIP) != 0 {
		ips = []string{options.bypassIP}
	} else {
		ips, err = parseFile(options.folder + "/ips")
		if err != nil {
			log.Printf("[!] Skipping headers technique (no IPs file): %v", err)
			return
		}
	}

	// Load simple headers for additional testing
	simpleHeaders, err := parseFile(options.folder + "/simpleheaders")
	if err != nil {
		log.Printf("[!] Simple headers unavailable, continuing without them: %v", err)
		simpleHeaders = nil
	}

	// Remove duplicates from lines and ips
	lines = removeDuplicates(lines)
	ips = removeDuplicates(ips)

	// Generate unique combinations of headers and IPs
	uniqueCombined := make(map[string]bool)
	var combined []struct {
		Line string
		IP   string
	}

	for _, ip := range ips {
		for _, line := range lines {
			key := line + ":" + ip
			if !uniqueCombined[key] {
				uniqueCombined[key] = true
				combined = append(combined, struct {
					Line string
					IP   string
				}{Line: line, IP: ip})
			}
		}
	}

	totalRequests := len(combined) + len(simpleHeaders)
	if totalRequests == 0 {
		return
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("headers", totalRequests)

	// Process combined headers
	for _, item := range combined {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(item struct {
			Line string
			IP   string
		}) {
			defer w.Done()
			defer p.done()

			// Add headers to the request
			headers := make([]header, len(options.headers))
			copy(headers, options.headers)
			headers = append(headers, header{item.Line, item.IP})

			statusCode, respSize, err := requestWithRetry(options.method, options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			result := Result{
				line:          item.Line + ": " + item.IP,
				statusCode:    statusCode,
				contentLength: respSize,
				defaultReq:    false,
			}
			printResponse(result, "headers")
		}(item)
	}

	// Process simple headers
	for _, simpleHeader := range simpleHeaders {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			defer w.Done()
			defer p.done()

			line = strings.TrimSpace(line)
			if line == "" {
				return
			}
			parts := strings.SplitN(line, " ", 2)
			if len(parts) < 2 {
				log.Printf("Invalid simple header format: %s\n", line)
				return
			}
			headers := make([]header, len(options.headers))
			copy(headers, options.headers)
			headers = append(headers, header{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])})

			statusCode, respSize, err := requestWithRetry(options.method, options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			result := Result{
				line:          line,
				statusCode:    statusCode,
				contentLength: respSize,
				defaultReq:    false,
			}
			printResponse(result, "headers")
		}(simpleHeader)
	}
	w.WaitAllDone()
	p.finish()
}

// Helper function to remove duplicates from a slice
func removeDuplicates(input []string) []string {
	uniqueMap := make(map[string]bool)
	var result []string
	for _, item := range input {
		if _, exists := uniqueMap[item]; !exists {
			uniqueMap[item] = true
			result = append(result, item)
		}
	}
	return result
}

// requestEndPaths makes HTTP requests using a list of custom end paths from a file and prints the results.
func requestEndPaths(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━━━━ CUSTOM PATHS ━━━━━━━━━━━━━━━━━")

	lines, err := parseFile(options.folder + "/endpaths")
	if err != nil {
		log.Printf("[!] Skipping endpaths technique: %v", err)
		return
	}
	if len(lines) == 0 {
		return
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("endpaths", len(lines))

	for _, line := range lines {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			defer w.Done()
			defer p.done()

			statusCode, respSize, err := requestWithRetry(options.method, joinURL(options.uri, line), options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			contentLength := respSize

			if isCalibrationMatch(contentLength) {
				return
			}

			result := Result{
				line:          joinURL(options.uri, line),
				statusCode:    statusCode,
				contentLength: contentLength,
				defaultReq:    false,
			}

			printResponse(result, "endpaths")
		}(line)
	}

	w.WaitAllDone()
	p.finish()
}

// requestMidPaths makes HTTP requests using a list of custom mid-paths from a file and prints the results.
func requestMidPaths(options RequestOptions) {
	lines, err := parseFile(options.folder + "/midpaths")
	if err != nil {
		log.Printf("[!] Skipping midpaths technique: %v", err)
		return
	}
	if len(lines) == 0 {
		return
	}

	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}

	pathValue := parsedURL.Path
	if pathValue == "" || pathValue == "/" {
		log.Println("No path to modify")
		return
	}

	trailingSlash := strings.HasSuffix(pathValue, "/")
	trimmedPath := strings.Trim(pathValue, "/")
	segments := strings.Split(trimmedPath, "/")
	if len(segments) == 0 {
		return
	}

	uripath := segments[len(segments)-1]
	baseSegments := segments[:len(segments)-1]
	basePath := "/"
	if len(baseSegments) > 0 {
		basePath = "/" + strings.Join(baseSegments, "/") + "/"
	}

	baseURL := parsedURL.Scheme + "://" + parsedURL.Host
	queryStr := ""
	if parsedURL.RawQuery != "" {
		queryStr = "?" + parsedURL.RawQuery
	}
	w := goccm.New(maxGoroutines)
	p := newProgress("midpaths", len(lines))

	for _, line := range lines {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			defer w.Done()
			defer p.done()

			fullpath := baseURL + basePath + line + uripath
			if trailingSlash {
				fullpath += "/"
			}
			fullpath += queryStr

			statusCode, respSize, err := requestWithRetry(options.method, fullpath, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			if isCalibrationMatch(respSize) {
				return
			}

			result := Result{
				line:          fullpath,
				statusCode:    statusCode,
				contentLength: respSize,
				defaultReq:    false,
			}
			printResponse(result, "midpaths")
		}(line)
	}
	w.WaitAllDone()
	p.finish()
}

// requestDoubleEncoding makes HTTP requests doing a double URL encode of the path
func requestDoubleEncoding(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━━━━ DOUBLE-ENCODING ━━━━━━━━━━━━━━")

	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}

	originalPath := parsedURL.Path
	if len(originalPath) == 0 || originalPath == "/" {
		log.Println("No path to modify")
		return
	}

	// Count non-slash characters for progress
	totalChars := 0
	for _, c := range originalPath {
		if c != '/' {
			totalChars++
		}
	}
	if totalChars == 0 {
		return
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("double-encoding", totalChars)

	for i, c := range originalPath {
		if c == '/' {
			continue
		}

		singleEncoded := fmt.Sprintf("%%%X", c)
		doubleEncoded := url.QueryEscape(singleEncoded)

		modifiedPathStr := originalPath[:i] + doubleEncoded + originalPath[i+len(string(c)):]

		encodedUri := fmt.Sprintf("%s://%s%s", parsedURL.Scheme, parsedURL.Host, modifiedPathStr)
		if parsedURL.RawQuery != "" {
			encodedUri += "?" + parsedURL.RawQuery
		}

		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(encodedUri string, modifiedChar rune) {
			defer w.Done()
			defer p.done()

			statusCode, respSize, err := requestWithRetry(options.method, encodedUri, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			result := Result{
				line:          encodedUri,
				statusCode:    statusCode,
				contentLength: respSize,
				defaultReq:    false,
			}
			printResponse(result, "double-encoding")
		}(encodedUri, c)
	}

	w.WaitAllDone()
	p.finish()
}

// requestUnicodeEncoding makes HTTP requests using Unicode/overlong UTF-8 encoded paths
func requestUnicodeEncoding(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━━ UNICODE ENCODING ━━━━━━━━━━━━━━━")

	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}

	originalPath := parsedURL.Path
	if len(originalPath) == 0 || originalPath == "/" {
		log.Println("No path to modify")
		return
	}

	baseURL := parsedURL.Scheme + "://" + parsedURL.Host
	query := ""
	if parsedURL.RawQuery != "" {
		query = "?" + parsedURL.RawQuery
	}

	// Generate Unicode/overlong encoding variations for the path
	var payloads []string

	// Overlong UTF-8 slash: %c0%af (overlong encoding of /)
	// Unicode slash: %u002f
	// These bypass filters that only decode standard percent-encoding
	overlongReplacements := []struct {
		original string
		encoded  string
	}{
		{"/", "%c0%af"},
		{"/", "%u002f"},
		{"/", "%e0%80%af"},
		{"/", "%f0%80%80%af"},
		{"/", "%252f"},
	}

	for _, r := range overlongReplacements {
		// Replace each slash in the path (except the leading one) with the encoded version
		// Only add if there are internal slashes to replace (otherwise it's a no-op duplicate)
		if len(originalPath) > 1 && strings.Contains(originalPath[1:], r.original) {
			modified := "/" + strings.ReplaceAll(originalPath[1:], r.original, r.encoded)
			payloads = append(payloads, baseURL+modified+query)
		}
	}

	// Also try encoding individual characters in the last path segment
	segments := strings.Split(strings.Trim(originalPath, "/"), "/")
	lastSegment := segments[len(segments)-1]
	basePath := "/"
	if len(segments) > 1 {
		basePath = "/" + strings.Join(segments[:len(segments)-1], "/") + "/"
	}

	for i, c := range lastSegment {
		// Unicode encoding: %uXXXX
		unicodeEncoded := fmt.Sprintf("%%u%04x", c)
		modified := basePath + lastSegment[:i] + unicodeEncoded + lastSegment[i+len(string(c)):]
		payloads = append(payloads, baseURL+modified+query)

		// Overlong UTF-8 2-byte encoding for ASCII chars:
		// byte1 = 0xC0 | (char >> 6), byte2 = 0x80 | (char & 0x3F)
		// e.g., '/' (0x2F) → %c0%af, 'a' (0x61) → %c1%a1
		if c < 128 {
			byte1 := byte(0xC0) | byte(c>>6)
			byte2 := byte(0x80) | byte(c&0x3F)
			overlongEncoded := fmt.Sprintf("%%%02x%%%02x", byte1, byte2)
			modified = basePath + lastSegment[:i] + overlongEncoded + lastSegment[i+len(string(c)):]
			payloads = append(payloads, baseURL+modified+query)
		}
	}

	if len(payloads) == 0 {
		return
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("unicode-encoding", len(payloads))

	for _, payload := range payloads {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(payload string) {
			defer w.Done()
			defer p.done()

			statusCode, respSize, err := requestWithRetry(options.method, payload, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			if isCalibrationMatch(respSize) {
				return
			}

			result := Result{
				line:          payload,
				statusCode:    statusCode,
				contentLength: respSize,
				defaultReq:    false,
			}
			printResponse(result, "unicode-encoding")
		}(payload)
	}

	w.WaitAllDone()
	p.finish()
}

// requestPayloadPositions makes HTTP requests injecting payloads at custom marked positions.
func requestPayloadPositions(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━ PAYLOAD POSITIONS ━━━━━━━━━━━━━━━")

	if len(options.payloadPositions) == 0 || options.uriTemplate == "" {
		log.Println("[!] No payload positions defined. Use --payload-position with markers in the URL (e.g., -p '§' -u 'http://example.com/§100§/admin').")
		return
	}

	// Load payloads from endpaths and midpaths files
	var payloads []string
	endPaths, err := parseFile(options.folder + "/endpaths")
	if err == nil {
		payloads = append(payloads, endPaths...)
	}
	midPaths, err := parseFile(options.folder + "/midpaths")
	if err == nil {
		payloads = append(payloads, midPaths...)
	}

	if len(payloads) == 0 {
		log.Println("[!] No payloads found for position injection")
		return
	}

	// For each position, replace the marked value with each payload
	type workItem struct {
		uri      string
		position int
		payload  string
	}
	var items []workItem

	for posIdx := range options.payloadPositions {
		for _, payload := range payloads {
			// Build the URI by replacing the specific position placeholder with the payload
			uri := options.uriTemplate
			for i, origValue := range options.payloadPositions {
				ph := payloadPlaceholder(i)
				if i == posIdx {
					uri = strings.Replace(uri, ph, payload, 1)
				} else {
					uri = strings.Replace(uri, ph, origValue, 1)
				}
			}
			items = append(items, workItem{uri: uri, position: posIdx, payload: payload})
		}
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("payload-position", len(items))

	for _, item := range items {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(item workItem) {
			defer w.Done()
			defer p.done()

			statusCode, respSize, err := requestWithRetry(options.method, item.uri, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			if isCalibrationMatch(respSize) {
				return
			}

			result := Result{
				line:          item.uri,
				statusCode:    statusCode,
				contentLength: respSize,
				defaultReq:    false,
			}
			printResponse(result, "payload-position")
		}(item)
	}

	w.WaitAllDone()
	p.finish()
}

// curlAvailable checks if curl is installed and accessible in PATH.
var curlChecked bool
var curlExists bool

func isCurlAvailable() bool {
	if curlChecked {
		return curlExists
	}
	curlChecked = true
	_, err := exec.LookPath("curl")
	curlExists = err == nil
	return curlExists
}

// requestHttpVersions makes HTTP requests using a list of HTTP versions from a file and prints the results.
func requestHttpVersions(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━━━━ HTTP VERSIONS ━━━━━━━━━━━━━━━━")

	if !isCurlAvailable() {
		log.Printf("[!] Skipping HTTP versions technique: curl not found in PATH")
		return
	}

	httpVersions := []string{"--http1.0"}
	proxyValue := ""
	if options.proxy != nil {
		proxyValue = options.proxy.String()
	}

	for _, version := range httpVersions {
		res := curlRequest(options.uri, proxyValue, version, options.timeout)
		printResponse(res, "http-versions")
	}
}

func curlRequest(uri string, proxy string, httpVersion string, timeout int) Result {
	args := []string{"-i", "-s", httpVersion}
	args = append(args, "-H", "User-Agent:")
	args = append(args, "-H", "Accept:")
	args = append(args, "-H", "Connection:")
	args = append(args, "-H", "Host:")
	if proxy != "" {
		args = append(args, "-x", proxy)
	}
	if redirect {
		args = append(args, "-L")
	}
	args = append(args, "--insecure")
	args = append(args, uri)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "curl", args...)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("[!] Curl command failed: %v", err)
		return Result{}
	}

	return parseCurlOutput(string(out), httpVersion)
}

func parseCurlOutput(output string, httpVersion string) Result {
	httpVersionOutput := strings.ReplaceAll(httpVersion, "--http", "HTTP/")

	responses := strings.Split(output, "\r\n\r\n")

	var proxyResponse, serverResponse string

	for _, response := range responses {
		if strings.Contains(response, "Connection established") {
			proxyResponse = response
		} else if strings.HasPrefix(response, "HTTP/") {
			serverResponse = response
		}
	}

	if serverResponse == "" {
		log.Println("[!] No valid HTTP server response found in curl output")
		return Result{}
	}

	totalResponseSize := len(output)

	if proxyResponse != "" {
		totalResponseSize -= len(proxyResponse) + len("\r\n\r\n")
	}

	lines := strings.SplitN(serverResponse, "\n", 2)
	if len(lines) < 1 {
		log.Printf("[!] Invalid server response format: %s", serverResponse)
		return Result{}
	}

	parts := strings.SplitN(lines[0], " ", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "HTTP/") {
		log.Printf("[!] Invalid status line: %s", lines[0])
		return Result{}
	}

	statusCode, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		log.Printf("[!] Error parsing status code: %v", err)
		return Result{}
	}

	return Result{
		line:          httpVersionOutput,
		statusCode:    statusCode,
		contentLength: totalResponseSize,
		defaultReq:    false,
	}
}

// requestPathCaseSwitching makes HTTP requests by capitalizing each letter in the last part of the URI.
func requestPathCaseSwitching(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━ PATH CASE SWITCHING ━━━━━━━━━━━━━")

	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}

	baseuri := parsedURL.Scheme + "://" + parsedURL.Host
	uripath := strings.Trim(parsedURL.Path, "/")
	queryStr := ""
	if parsedURL.RawQuery != "" {
		queryStr = "?" + parsedURL.RawQuery
	}

	if len(uripath) == 0 {
		log.Println("No path to modify")
		return
	}

	pathCombinations := generateCaseCombinations(uripath)
	selectedPaths := selectRandomCombinations(pathCombinations, 20)
	w := goccm.New(maxGoroutines)
	p := newProgress("path-case-switching", len(selectedPaths))

	for _, path := range selectedPaths {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(path string) {
			defer w.Done()
			defer p.done()

			var fullpath string
			if strings.HasSuffix(options.uri, "/") {
				fullpath = baseuri + "/" + path + "/"
			} else {
				fullpath = baseuri + "/" + path
			}
			fullpath += queryStr

			statusCode, respSize, err := requestWithRetry(options.method, fullpath, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			result := Result{
				line:          fullpath,
				statusCode:    statusCode,
				contentLength: respSize,
				defaultReq:    false,
			}

			printResponse(result, "path-case-switching")
		}(path)
	}

	w.WaitAllDone()
	p.finish()
}

// randomLine take a random line from a file
func randomLine(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func(file *os.File) {
		_ = file.Close()
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
		return "", err
	}

	if len(lines) == 0 {
		return "", fmt.Errorf("no entries found in %s", filePath)
	}

	// Select a random Line
	randomLine := lines[rand.Intn(len(lines))]

	return randomLine, nil
}

// joinURL safely joins a base URL and a path, preserving slashes
func joinURL(base string, path string) string {
	if !strings.HasSuffix(base, "/") && !strings.HasPrefix(path, "/") {
		return base + "/" + path
	}
	if strings.HasSuffix(base, "/") && strings.HasPrefix(path, "/") {
		return base + path[1:]
	}
	return base + path
}

// validateURI checks that the URI is a valid HTTP(S) URL.
func validateURI(uri string) error {
	if uri == "" {
		return fmt.Errorf("URI is empty")
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid URI %q: %w", uri, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid URI scheme %q (must be http or https): %s", parsed.Scheme, uri)
	}
	if parsed.Host == "" {
		return fmt.Errorf("URI has no host: %s", uri)
	}
	return nil
}

// setupRequestOptions configures and returns RequestOptions based on the provided parameters.
func setupRequestOptions(uri, proxy, userAgent string, reqHeaders []string, bypassIP, folder, method string, verbose bool, techniques []string, banner, rateLimit bool, timeout int, redirect, randomAgent bool) RequestOptions {
	// Set up proxy if provided.
	if len(proxy) != 0 {
		if !strings.Contains(proxy, "http") {
			proxy = "http://" + proxy
		}
	}
	userProxy, err := url.Parse(proxy)
	if err != nil {
		log.Printf("[!] Error parsing proxy URL: %v, continuing without proxy", err)
		userProxy = &url.URL{}
	}

	// Check if URI has trailing slash, if not add it.
	x := strings.Split(uri, "/")
	if len(x) < 4 {
		uri += "/"
	}

	// Set User-Agent header.
	if !randomAgent {
		if len(userAgent) == 0 {
			userAgent = "nomore403"
		}
	} else {
		line, err := randomLine(folder + "/useragents")
		if err != nil {
			log.Printf("Error reading user agents file: %v", err)
			userAgent = "nomore403"
		} else {
			userAgent = line
		}
	}

	// Set default request method to GET.
	if len(method) == 0 {
		method = "GET"
	}

	headers := []header{
		{"User-Agent", userAgent},
	}

	// Parse custom headers from CLI arguments and add them to the headers slice.
	if len(reqHeaders) > 0 && reqHeaders[0] != "" {
		for _, _header := range reqHeaders {
			headerSplit := strings.SplitN(_header, ":", 2)
			if len(headerSplit) < 2 {
				log.Printf("Invalid header format: %s\n", _header)
				continue
			}
			headers = append(headers, header{strings.TrimSpace(headerSplit[0]), strings.TrimSpace(headerSplit[1])})
		}
	}

	opts := RequestOptions{
		uri:           uri,
		headers:       headers,
		method:        method,
		proxy:         userProxy,
		userAgent:     userAgent,
		redirect:      redirect,
		folder:        folder,
		bypassIP:      bypassIP,
		timeout:       timeout,
		rateLimit:     rateLimit,
		verbose:       verbose,
		techniques:    techniques,
		reqHeaders:    reqHeaders,
		banner:        banner,
		autocalibrate: !verbose,
	}

	// Parse payload position markers from URI
	if payloadPosition != "" {
		positions, template := parsePayloadPositions(uri, payloadPosition)
		if len(positions) > 0 {
			opts.payloadPositions = positions
			opts.uriTemplate = template
		}
	}

	return opts
}

// payloadPlaceholderPrefix is used to create unique placeholders that won't collide with URL content.
const payloadPlaceholderPrefix = "\x00PP_"

// parsePayloadPositions extracts marked segments from a URI template.
// Given a marker like "§" and URI "http://example.com/§100§/user/§200§",
// it returns the marked values ["100", "200"] and a template with internal placeholders.
func parsePayloadPositions(uri, marker string) ([]string, string) {
	var positions []string
	template := uri
	idx := 0

	for {
		start := strings.Index(template, marker)
		if start == -1 {
			break
		}
		rest := template[start+len(marker):]
		end := strings.Index(rest, marker)
		if end == -1 {
			break
		}
		value := rest[:end]
		positions = append(positions, value)
		placeholder := fmt.Sprintf("%s%d\x00", payloadPlaceholderPrefix, idx)
		template = template[:start] + placeholder + rest[end+len(marker):]
		idx++
	}

	return positions, template
}

// payloadPlaceholder returns the placeholder string for a given index.
func payloadPlaceholder(idx int) string {
	return fmt.Sprintf("%s%d\x00", payloadPlaceholderPrefix, idx)
}

// resetMaps clears all result tracking maps before starting new requests.
func resetMaps() {
	uniqueResultsMutex.Lock()
	for k := range uniqueResults {
		delete(uniqueResults, k)
	}
	uniqueResultsMutex.Unlock()

	uniqueResultsByTechMutex.Lock()
	for k := range uniqueResultsByTechnique {
		delete(uniqueResultsByTechnique, k)
	}
	uniqueResultsByTechMutex.Unlock()

	techSignatureCountsMutex.Lock()
	for k := range techSignatureCounts {
		delete(techSignatureCounts, k)
	}
	techSignatureCountsMutex.Unlock()

	for k := range verbTamperingResults {
		delete(verbTamperingResults, k)
	}
}

// executeTechniques runs the selected bypass techniques based on the provided options.
func executeTechniques(options RequestOptions) {
	for _, tech := range options.techniques {
		switch tech {
		case "verbs":
			requestMethods(options)
			printSuppressedSummary("verb-tampering")
		case "verbs-case":
			requestMethodsCaseSwitching(options)
			printSuppressedSummary("verb-tampering-case")
		case "headers":
			requestHeaders(options)
			printSuppressedSummary("headers")
		case "endpaths":
			requestEndPaths(options)
			printSuppressedSummary("endpaths")
		case "midpaths":
			requestMidPaths(options)
			printSuppressedSummary("midpaths")
		case "double-encoding":
			requestDoubleEncoding(options)
			printSuppressedSummary("double-encoding")
		case "http-versions":
			requestHttpVersions(options)
		case "path-case":
			requestPathCaseSwitching(options)
			printSuppressedSummary("path-case-switching")
		case "unicode":
			requestUnicodeEncoding(options)
			printSuppressedSummary("unicode-encoding")
		case "payload-position":
			requestPayloadPositions(options)
			printSuppressedSummary("payload-position")
		default:
			fmt.Printf("Unrecognized technique. %s\n", tech)
			fmt.Print("Available techniques: verbs, verbs-case, headers, endpaths, midpaths, double-encoding, unicode, http-versions, path-case\n")
		}
	}
}

// requester is the main function that runs all the tests.
func requester(uri string, proxy string, userAgent string, reqHeaders []string, bypassIP string, folder string, method string, verbose bool, techniques []string, banner bool, rateLimit bool, timeout int, redirect bool, randomAgent bool) {
	setVerbose(verbose)
	setDefaultSc(0)
	setDefaultRespCl(0)

	// Validate URI before proceeding
	if err := validateURI(uri); err != nil {
		log.Printf("[!] %v", err)
		return
	}

	options := setupRequestOptions(uri, proxy, userAgent, reqHeaders, bypassIP, folder, method, verbose, techniques, banner, rateLimit, timeout, redirect, randomAgent)

	// Auto-add payload-position technique if positions were detected
	if len(options.payloadPositions) > 0 {
		hasTech := false
		for _, t := range options.techniques {
			if t == "payload-position" {
				hasTech = true
				break
			}
		}
		if !hasTech {
			options.techniques = append(options.techniques, "payload-position")
		}
		// Build clean URI with original values (no markers) for other techniques
		cleanURI := options.uriTemplate
		for i, val := range options.payloadPositions {
			cleanURI = strings.Replace(cleanURI, payloadPlaceholder(i), val, 1)
		}
		options.uri = cleanURI
	}

	resetMaps()

	if maxGoroutines > 100 {
		log.Printf("Warning: High number of goroutines (%d) may cause resource exhaustion.", maxGoroutines)
	}

	// Display configuration and perform auto-calibration
	showInfo(options)

	// Auto-calibrate with multi-sample tolerance
	if options.autocalibrate {
		avgCl, tolerance := runAutocalibrate(options)
		setDefaultCl(avgCl)
		setCalibTolerance(tolerance)
	}

	requestDefault(options)
	executeTechniques(options)
}
