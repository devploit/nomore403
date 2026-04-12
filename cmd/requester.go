package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"math/rand"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/fatih/color"
	"github.com/zenthangplus/goccm"
	"golang.org/x/term"
)

type Result struct {
	line          string
	statusCode    int
	contentLength int
	defaultReq    bool
	bodyHash      string
	location      string
	contentType   string
	server        string
	technique     string
	reproCurl     string
	score         int
	likelihood    string
	scoreReason   string
	relatedCount  int
	familyKey     string
	revalidated   int
	replayMatches int
	replay        *ReplaySpec
}

type ResponseInfo struct {
	statusCode    int
	contentLength int
	bodyHash      string
	location      string
	contentType   string
	server        string
	via           string
	xCache        string
	poweredBy     string
	cfRay         string
}

type ReplaySpec struct {
	kind          string
	method        string
	uri           string
	headers       []header
	body          string
	redirect      bool
	proxy         string
	timeout       int
	requestTarget string
	curlArgs      []string
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
	strictCalibrate  bool
	rawHTTP          bool
	frontendHints    []string
	payloadPositions []string // extracted marked segments from URL template
	uriTemplate      string   // original URI with markers for payload injection
}

// Thread-safe globals using atomic operations.
var atomicVerbose int32
var atomicDefaultCl int64
var atomicCalibTolerance int64
var atomicDefaultSc int32     // default request status code
var atomicDefaultRespCl int64 // default request content-length (separate from calibration)
var atomicFragmentCl int64    // fragment baseline content-length (URI#fragment response)

func getVerbose() bool { return atomic.LoadInt32(&atomicVerbose) != 0 }
func logVerbose(v ...interface{}) {
	if getVerbose() {
		log.Println(v...)
	}
}
func setVerbose(v bool) {
	var val int32
	if v {
		val = 1
	}
	atomic.StoreInt32(&atomicVerbose, val)
}
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
func getFragmentCl() int     { return int(atomic.LoadInt64(&atomicFragmentCl)) }
func setFragmentCl(v int)    { atomic.StoreInt64(&atomicFragmentCl, int64(v)) }

// isCalibrationMatch returns true if the content length is within tolerance of the calibrated default
// or the fragment baseline (URI#fragment response, which catches fragment-stripped false positives).
// In verbose mode, always returns false so all results are shown.
func isCalibrationMatch(contentLength int) bool {
	if getVerbose() {
		return false
	}
	tolerance := getCalibTolerance()

	// Check against calibration baseline (404 pages)
	cl := getDefaultCl()
	if cl > 0 {
		diff := contentLength - cl
		if diff < 0 {
			diff = -diff
		}
		if diff <= tolerance {
			return true
		}
	}

	// Check against fragment baseline (URI#fragment → parent path response)
	fragCl := getFragmentCl()
	if fragCl > 0 {
		diff := contentLength - fragCl
		if diff < 0 {
			diff = -diff
		}
		if diff <= tolerance {
			return true
		}
	}

	return false
}

func resultFromResponse(line string, defaultReq bool, technique string, resp ResponseInfo) Result {
	return Result{
		line:          line,
		statusCode:    resp.statusCode,
		contentLength: resp.contentLength,
		defaultReq:    defaultReq,
		bodyHash:      resp.bodyHash,
		location:      resp.location,
		contentType:   resp.contentType,
		server:        resp.server,
		technique:     technique,
	}
}

var storedGlobalBaseline ResponseInfo

func globalBaseline() ResponseInfo {
	techniqueBaselinesMutex.Lock()
	defer techniqueBaselinesMutex.Unlock()
	if storedGlobalBaseline.statusCode != 0 || storedGlobalBaseline.contentLength != 0 || storedGlobalBaseline.bodyHash != "" {
		return storedGlobalBaseline
	}
	return ResponseInfo{
		statusCode:    getDefaultSc(),
		contentLength: getDefaultRespCl(),
	}
}

func setGlobalBaseline(resp ResponseInfo) {
	techniqueBaselinesMutex.Lock()
	defer techniqueBaselinesMutex.Unlock()
	storedGlobalBaseline = resp
}

func setTechniqueBaseline(technique string, resp ResponseInfo) {
	techniqueBaselinesMutex.Lock()
	defer techniqueBaselinesMutex.Unlock()
	techniqueBaselines[technique] = resp
}

func techniqueBaseline(technique string) (ResponseInfo, bool) {
	techniqueBaselinesMutex.Lock()
	defer techniqueBaselinesMutex.Unlock()
	resp, ok := techniqueBaselines[technique]
	return resp, ok
}

func baselineForTechnique(technique string) ResponseInfo {
	if resp, ok := techniqueBaseline(technique); ok {
		return resp
	}
	return globalBaseline()
}

func isInteresting(result Result) bool {
	baseline := baselineForTechnique(result.technique)

	if baseline.statusCode == 0 && baseline.contentLength == 0 && baseline.bodyHash == "" {
		return true
	}

	if result.statusCode != baseline.statusCode {
		return true
	}

	tolerance := getCalibTolerance()
	if tolerance == 0 {
		tolerance = calibrationTolerance
	}
	diff := result.contentLength - baseline.contentLength
	if diff < 0 {
		diff = -diff
	}
	if diff > tolerance {
		return true
	}

	if strictCalibrate {
		if baseline.bodyHash != "" && result.bodyHash != "" && baseline.bodyHash != result.bodyHash {
			return true
		}
		if baseline.location != result.location {
			return true
		}
		if baseline.contentType != result.contentType {
			return true
		}
		if baseline.server != result.server {
			return true
		}
	}

	return false
}

func classifyLikelihood(score int) string {
	switch {
	case score >= 80:
		return "high"
	case score >= 55:
		return "medium"
	default:
		return "low"
	}
}

func statusPriority(status int) int {
	switch {
	case status >= 200 && status < 300:
		return 5
	case status >= 300 && status < 400:
		return 4
	case status == 401 || status == 403:
		return 2
	case status >= 400 && status < 500:
		return 1
	case status >= 500:
		return 0
	default:
		return 1
	}
}

func absoluteDifference(a, b int) int {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff
}

func hasBodyChanged(baseline ResponseInfo, result Result) bool {
	return baseline.bodyHash != "" && result.bodyHash != "" && baseline.bodyHash != result.bodyHash
}

func looksLikeAccessControlRedirect(location string) bool {
	loc := strings.ToLower(location)
	for _, token := range []string{"login", "signin", "sign-in", "auth", "forbidden", "denied", "unauthorized", "403", "error"} {
		if strings.Contains(loc, token) {
			return true
		}
	}
	return false
}

func hasAnomalousRedirect(result Result, baseline ResponseInfo) bool {
	if result.statusCode < 300 || result.statusCode >= 400 || result.location == "" {
		return false
	}
	if looksLikeAccessControlRedirect(result.location) {
		return false
	}
	if baseline.location != "" && baseline.location != result.location {
		return true
	}
	if baseline.statusCode < 300 || baseline.statusCode >= 400 {
		if result.contentLength == 0 {
			return false
		}
		return true
	}
	location := strings.ToLower(result.location)
	return strings.HasPrefix(location, "/") || strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://")
}

func isSameStatusEmptyBody(result Result, baseline ResponseInfo) bool {
	return baseline.statusCode != 0 && baseline.statusCode == result.statusCode && result.contentLength == 0
}

func statusTransition(result Result) string {
	base := baselineForTechnique(result.technique).statusCode
	if base == 0 {
		return strconv.Itoa(result.statusCode)
	}
	return fmt.Sprintf("%d->%d", base, result.statusCode)
}

func colorizeStatusCode(status int) string {
	switch {
	case status >= 200 && status < 300:
		return color.GreenString(strconv.Itoa(status))
	case status >= 300 && status < 400:
		return color.YellowString(strconv.Itoa(status))
	case status == 400 || status == 401 || status == 403 || status == 404:
		return color.RedString(strconv.Itoa(status))
	case status == 405 || status == 406 || status == 407 || status == 408 || status == 413 || status == 429:
		return color.New(color.FgHiMagenta).Sprint(strconv.Itoa(status))
	case status >= 400 && status < 500:
		return color.New(color.FgMagenta).Sprint(strconv.Itoa(status))
	case status >= 500:
		return color.MagentaString(strconv.Itoa(status))
	default:
		return strconv.Itoa(status)
	}
}

func colorizeStatusTransition(result Result) string {
	base := baselineForTechnique(result.technique).statusCode
	if base == 0 {
		return colorizeStatusCode(result.statusCode)
	}

	arrow := "->"
	if statusPriority(result.statusCode) > statusPriority(base) {
		arrow = color.GreenString("=>")
	} else if statusPriority(result.statusCode) < statusPriority(base) {
		arrow = color.HiBlackString("->")
	}

	return fmt.Sprintf("%s%s%s", colorizeStatusCode(base), arrow, colorizeStatusCode(result.statusCode))
}

func scoreReason(result Result) string {
	baseline := baselineForTechnique(result.technique)
	var reasons []string
	accessControlRedirect := looksLikeAccessControlRedirect(result.location)

	if baseline.statusCode != 0 && result.statusCode != baseline.statusCode {
		reasons = append(reasons, fmt.Sprintf("status %d->%d", baseline.statusCode, result.statusCode))
	}

	tolerance := getCalibTolerance()
	if tolerance == 0 {
		tolerance = calibrationTolerance
	}
	diff := absoluteDifference(result.contentLength, baseline.contentLength)
	if diff > tolerance {
		reasons = append(reasons, fmt.Sprintf("len Δ%d", diff))
	}
	if hasBodyChanged(baseline, result) {
		reasons = append(reasons, "body changed")
	}
	if baseline.location != result.location {
		reasons = append(reasons, "location changed")
	}
	if baseline.contentType != "" && baseline.contentType != result.contentType {
		reasons = append(reasons, "type changed")
	}
	if baseline.server != "" && baseline.server != result.server {
		reasons = append(reasons, "server changed")
	}
	if hasAnomalousRedirect(result, baseline) {
		reasons = append(reasons, "redirect anomaly")
	}
	if accessControlRedirect {
		reasons = append(reasons, "redirect to access control")
	}
	if len(reasons) == 0 {
		return "minor variation"
	}
	return strings.Join(reasons, ", ")
}

func scoreResult(result Result) int {
	baseline := baselineForTechnique(result.technique)
	score := 0
	basePriority := statusPriority(baseline.statusCode)
	resultPriority := statusPriority(result.statusCode)
	accessControlRedirect := looksLikeAccessControlRedirect(result.location)
	anomalousRedirect := hasAnomalousRedirect(result, baseline)

	switch {
	case result.statusCode >= 200 && result.statusCode < 300:
		score += 55
	case result.statusCode >= 300 && result.statusCode < 400:
		score += 22
	case result.statusCode == 401 || result.statusCode == 403:
		score += 4
	case result.statusCode == 400 || result.statusCode == 404:
		score -= 2
	case result.statusCode >= 400 && result.statusCode < 500:
		score -= 3
	}

	if baseline.statusCode != 0 && result.statusCode != baseline.statusCode {
		if resultPriority > basePriority {
			score += 30
		} else if resultPriority == basePriority {
			score += 8
		} else {
			score -= 4
		}
	}

	tolerance := getCalibTolerance()
	if tolerance == 0 {
		tolerance = calibrationTolerance
	}
	diff := absoluteDifference(result.contentLength, baseline.contentLength)
	switch {
	case diff > tolerance*4:
		score += 22
	case diff > tolerance*2:
		score += 14
	case diff > tolerance:
		score += 8
	}

	if hasBodyChanged(baseline, result) {
		score += 10
	}
	if baseline.location != result.location {
		score += 6
	}
	if baseline.contentType != "" && baseline.contentType != result.contentType {
		score += 5
	}
	if baseline.server != "" && baseline.server != result.server {
		score += 4
	}
	if anomalousRedirect {
		score += 10
	}
	if result.statusCode >= 300 && result.statusCode < 400 && result.contentLength == 0 {
		score -= 12
	}
	if accessControlRedirect {
		score -= 22
	}
	if result.statusCode >= 300 && result.statusCode < 400 && (!anomalousRedirect || accessControlRedirect) {
		if score > 24 {
			score = 24
		}
	}
	if baseline.statusCode == result.statusCode {
		if hasBodyChanged(baseline, result) && result.contentLength > 0 {
			score += 14
		}
		if !isSameStatusEmptyBody(result, baseline) {
			switch {
			case diff > tolerance*4:
				score += 10
			case diff > tolerance*2:
				score += 6
			case diff > tolerance:
				score += 3
			}
		}
		if baseline.contentType != "" && baseline.contentType != result.contentType {
			score += 6
		}
		if baseline.location != "" && baseline.location != result.location {
			score += 8
		}
		if baseline.server != "" && baseline.server != result.server {
			score += 3
		}
	}
	if isSameStatusEmptyBody(result, baseline) {
		score -= 18
	}

	if isCalibrationMatch(result.contentLength) && result.statusCode == baseline.statusCode {
		score -= 20
	}
	if (result.statusCode == 400 || result.statusCode == 404) && diff <= tolerance && result.bodyHash == baseline.bodyHash {
		score -= 10
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

func buildCurlCommand(method, uri string, headers []header, body string, redirect bool, proxy *url.URL) string {
	var parts []string
	parts = append(parts, "curl", "-i", "-sS", "-k")
	if redirect {
		parts = append(parts, "-L")
	}
	if method != "" && method != "GET" {
		parts = append(parts, "-X", shellQuote(method))
	}
	if proxy != nil && proxy.String() != "" {
		parts = append(parts, "-x", shellQuote(proxy.String()))
	}
	for _, h := range headers {
		parts = append(parts, "-H", shellQuote(h.key+": "+h.value))
	}
	if body != "" {
		parts = append(parts, "--data", shellQuote(body))
	}
	parts = append(parts, shellQuote(uri))
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func cloneHeaders(headers []header) []header {
	cp := make([]header, len(headers))
	copy(cp, headers)
	return cp
}

func setTechniqueBaselines(resp ResponseInfo, techniques ...string) {
	for _, technique := range techniques {
		setTechniqueBaseline(technique, resp)
	}
}

func responseInfoFromResult(result Result) ResponseInfo {
	return ResponseInfo{
		statusCode:    result.statusCode,
		contentLength: result.contentLength,
		bodyHash:      result.bodyHash,
		location:      result.location,
		contentType:   result.contentType,
		server:        result.server,
	}
}

func attachHTTPReplay(result *Result, method, uri string, headers []header, body string, redirect bool, proxy *url.URL, timeout int) {
	result.reproCurl = buildCurlCommand(method, uri, headers, body, redirect, proxy)
	proxyValue := ""
	if proxy != nil {
		proxyValue = proxy.String()
	}
	result.replay = &ReplaySpec{
		kind:     "http",
		method:   method,
		uri:      uri,
		headers:  cloneHeaders(headers),
		body:     body,
		redirect: redirect,
		proxy:    proxyValue,
		timeout:  timeout,
	}
}

func attachRawReplay(result *Result, method, uri, requestTarget string, headers []header, body string, timeout int) {
	result.reproCurl = buildCurlCommand(method, uri, headers, body, false, nil)
	result.replay = &ReplaySpec{
		kind:          "raw",
		method:        method,
		uri:           uri,
		headers:       cloneHeaders(headers),
		body:          body,
		timeout:       timeout,
		requestTarget: requestTarget,
	}
}

func attachCurlReplay(result *Result, curlArgs []string, display string, timeout int) {
	result.reproCurl = display
	result.replay = &ReplaySpec{
		kind:     "curl",
		curlArgs: append([]string(nil), curlArgs...),
		timeout:  timeout,
	}
}

func techniqueFamily(technique string) string {
	switch technique {
	case "verb-tampering", "verb-tampering-case":
		return "method"
	case "headers", "headers-ip", "headers-simple", "headers-host", "hop-by-hop", "header-confusion", "forwarded-trust", "proto-confusion", "ip-encoding":
		return "trust-header"
	case "host-override":
		return "host-header"
	case "endpaths", "midpaths", "double-encoding", "unicode-encoding", "path-case-switching", "path-normalization", "payload-position":
		return "path"
	case "suffix-tricks":
		return "suffix"
	case "method-override-query", "method-override-header", "method-override-body":
		return "override"
	case "absolute-uri", "http-versions", "http-parser", "raw-authority", "raw-duplicates", "raw-desync":
		return "frontend-raw"
	default:
		return technique
	}
}

func resultFamilyKey(result Result) string {
	bodySig := result.bodyHash
	if bodySig == "" {
		bodySig = strconv.Itoa(result.contentLength)
	}
	return strings.Join([]string{
		techniqueFamily(result.technique),
		strconv.Itoa(result.statusCode),
		bodySig,
		result.location,
		result.contentType,
	}, "|")
}

func inferFrontendHints(uri string, resp ResponseInfo) []string {
	var hints []string
	parsed, _ := url.Parse(uri)
	host := ""
	if parsed != nil {
		host = strings.ToLower(parsed.Host)
	}
	server := strings.ToLower(resp.server)
	via := strings.ToLower(resp.via)
	xcache := strings.ToLower(resp.xCache)

	addHint := func(h string) {
		for _, existing := range hints {
			if existing == h {
				return
			}
		}
		hints = append(hints, h)
	}

	if strings.Contains(host, "elb.amazonaws.com") || strings.Contains(server, "awselb") {
		addHint("AWS ELB/ALB")
	}
	if strings.Contains(via, "cloudfront") || strings.Contains(xcache, "cloudfront") {
		addHint("CloudFront")
	}
	if resp.cfRay != "" || strings.Contains(server, "cloudflare") {
		addHint("Cloudflare")
	}
	if strings.Contains(server, "nginx") {
		addHint("Nginx")
	}
	if strings.Contains(server, "envoy") {
		addHint("Envoy")
	}
	if strings.Contains(server, "apache") {
		addHint("Apache")
	}
	if strings.Contains(server, "iis") {
		addHint("IIS")
	}
	if resp.poweredBy != "" {
		addHint(resp.poweredBy)
	}
	return hints
}

func prioritizeTechniques(techniques []string, hints []string) []string {
	if len(hints) == 0 {
		return techniques
	}
	priority := map[string]int{}
	for _, hint := range hints {
		switch hint {
		case "AWS ELB/ALB", "CloudFront", "Cloudflare", "Envoy", "Nginx":
			for _, tech := range []string{"absolute-uri", "header-confusion", "forwarded-trust", "proto-confusion", "host-override", "raw-authority", "headers", "path-normalization", "suffix-tricks", "http-parser"} {
				priority[tech] += 10
			}
		case "IIS":
			for _, tech := range []string{"unicode", "double-encoding", "path-normalization", "raw-authority", "suffix-tricks"} {
				priority[tech] += 10
			}
		}
	}
	ordered := append([]string(nil), techniques...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return priority[ordered[i]] > priority[ordered[j]]
	})
	return ordered
}

var uniqueResults = make(map[string]bool)
var uniqueResultsByTechnique = make(map[string]map[string]bool)
var verbTamperingResults = make(map[string]int)
var techniqueBaselines = make(map[string]ResponseInfo)
var topFindings []Result
var printedBaselineHeader bool
var printedFindingsHeader bool
var printedTechniqueHeaders = make(map[string]bool)
var executedTechniques = make(map[string]bool)
var producedTechniques = make(map[string]bool)
var seenTechniqueFamilies = make(map[string]bool)
var suppressedTechniqueFamilies = make(map[string]int)
var seenGlobalFamilies = make(map[string]int)
var suppressedCrossTechniqueFamilies = make(map[string]int)

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
	techniqueBaselinesMutex  = &sync.Mutex{}
	topFindingsMutex         = &sync.Mutex{}
	techniqueHeadersMutex    = &sync.Mutex{}
)

var techniqueTitles = map[string]string{
	"default":                "DEFAULT REQUEST",
	"verb-tampering":         "VERB TAMPERING",
	"verb-tampering-case":    "VERB TAMPERING CASE SWITCHING",
	"headers":                "HEADERS",
	"endpaths":               "CUSTOM PATHS",
	"midpaths":               "MID-PATH MUTATIONS",
	"double-encoding":        "DOUBLE-ENCODING",
	"unicode-encoding":       "UNICODE ENCODING",
	"payload-position":       "PAYLOAD POSITIONS",
	"http-versions":          "HTTP VERSIONS",
	"http-parser":            "HTTP PARSER",
	"path-case-switching":    "PATH CASE SWITCHING",
	"hop-by-hop":             "HOP-BY-HOP",
	"absolute-uri":           "ABSOLUTE URI",
	"method-override-query":  "METHOD QUERY OVERRIDE",
	"method-override-header": "METHOD HEADER OVERRIDE",
	"method-override-body":   "METHOD BODY OVERRIDE",
	"path-normalization":     "PATH NORMALIZATION",
	"suffix-tricks":          "SUFFIX TRICKS",
	"header-confusion":       "HEADER CONFUSION",
	"host-override":          "HOST OVERRIDE",
	"forwarded-trust":        "FORWARDED TRUST",
	"proto-confusion":        "PROTO CONFUSION",
	"ip-encoding":            "IP ENCODING",
	"raw-duplicates":         "RAW DUPLICATES",
	"raw-authority":          "RAW AUTHORITY",
	"raw-desync":             "RAW DESYNC",
}

var techniqueLabels = map[string]string{
	"default":                "Default request",
	"verb-tampering":         "Verb tampering",
	"verb-tampering-case":    "Verb case switching",
	"headers":                "Header injection",
	"headers-ip":             "Header injection (IP)",
	"headers-simple":         "Header injection (simple)",
	"headers-host":           "Header injection (host)",
	"endpaths":               "Custom paths",
	"midpaths":               "Mid-path mutations",
	"double-encoding":        "Double-encoding",
	"unicode-encoding":       "Unicode encoding",
	"payload-position":       "Payload positions",
	"http-versions":          "HTTP versions",
	"http-parser":            "HTTP parser",
	"path-case-switching":    "Path case switching",
	"hop-by-hop":             "Hop-by-hop",
	"absolute-uri":           "Absolute URI",
	"method-override-query":  "Method query override",
	"method-override-header": "Method header override",
	"method-override-body":   "Method body override",
	"path-normalization":     "Path normalization",
	"suffix-tricks":          "Suffix tricks",
	"header-confusion":       "Header confusion",
	"host-override":          "Host override",
	"forwarded-trust":        "Forwarded trust",
	"proto-confusion":        "Proto confusion",
	"ip-encoding":            "IP encoding",
	"raw-duplicates":         "Raw duplicates",
	"raw-authority":          "Raw authority",
	"raw-desync":             "Raw desync",
}

var techniqueAliases = map[string]string{
	"default":                "default",
	"verb-tampering":         "verbs",
	"verb-tampering-case":    "verbs-case",
	"headers":                "headers",
	"headers-ip":             "hdr-ip",
	"headers-simple":         "hdr-simple",
	"headers-host":           "hdr-host",
	"endpaths":               "endpaths",
	"midpaths":               "midpaths",
	"double-encoding":        "dbl-enc",
	"unicode-encoding":       "unicode",
	"payload-position":       "payload-pos",
	"http-versions":          "http",
	"http-parser":            "parser",
	"path-case-switching":    "path-case",
	"hop-by-hop":             "hop-hop",
	"absolute-uri":           "abs-uri",
	"method-override-query":  "mo-query",
	"method-override-header": "mo-header",
	"method-override-body":   "mo-body",
	"path-normalization":     "normalize",
	"suffix-tricks":          "suffix",
	"header-confusion":       "hdr-conf",
	"host-override":          "host",
	"forwarded-trust":        "forwarded",
	"proto-confusion":        "proto",
	"ip-encoding":            "ip-enc",
	"raw-duplicates":         "raw-dupe",
	"raw-authority":          "raw-auth",
	"raw-desync":             "raw-sync",
}

// printResponse prints the results of HTTP requests in a tabular format with colored output based on the status codes.
func printResponse(result Result, technique string) {
	printMutex.Lock()
	defer printMutex.Unlock()
	result.technique = technique
	result.score = scoreResult(result)
	result.likelihood = classifyLikelihood(result.score)
	result.scoreReason = scoreReason(result)
	result.familyKey = resultFamilyKey(result)

	// If verbose mode is enabled, directly print the result
	if getVerbose() || technique == "http-versions" || technique == "http-parser" {
		printResult(result)
		writeResultToOutput(result, technique)
		markTechniqueProduced(technique)
		recordFinding(result)
		return
	}

	// In non-verbose (smart) mode: only show results that differ from the default response.
	// Default request itself is always shown.
	if !result.defaultReq && !isInteresting(result) {
		return
	}
	if !result.defaultReq && result.score == 0 {
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

	if !result.defaultReq && result.familyKey != "" {
		familyKey := technique + ":" + result.familyKey
		techniqueHeadersMutex.Lock()
		if seenTechniqueFamilies[familyKey] {
			suppressedTechniqueFamilies[technique]++
			techniqueHeadersMutex.Unlock()
			return
		}
		seenTechniqueFamilies[familyKey] = true
		techniqueHeadersMutex.Unlock()
	}

	if !result.defaultReq && result.familyKey != "" {
		techniqueHeadersMutex.Lock()
		seenScore, exists := seenGlobalFamilies[result.familyKey]
		if exists && result.score <= seenScore+5 {
			suppressedCrossTechniqueFamilies[technique]++
			techniqueHeadersMutex.Unlock()
			if result.score >= variationScoreMin {
				recordFinding(result)
			}
			return
		}
		if !exists || result.score > seenScore {
			seenGlobalFamilies[result.familyKey] = result.score
		}
		techniqueHeadersMutex.Unlock()
	}

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
	markTechniqueProduced(technique)
	recordFinding(result)
}

func ensureTechniqueHeaderLocked(technique string) {
	if printedTechniqueHeaders[technique] {
		return
	}
	title, ok := techniqueTitles[technique]
	if !ok {
		return
	}
	clearActiveProgress()
	fmt.Println(color.CyanString("\n" + formatSectionHeader(title)))
	redrawActiveProgress()
	printedTechniqueHeaders[technique] = true
}

func formatSectionHeader(title string) string {
	const totalWidth = 40
	if len(title) >= totalWidth-2 {
		return "━━ " + title + " ━━"
	}
	remaining := totalWidth - len(title) - 2
	left := remaining / 2
	right := remaining - left
	return strings.Repeat("━", left) + " " + title + " " + strings.Repeat("━", right)
}

func markTechniqueExecuted(technique string) {
	techniqueHeadersMutex.Lock()
	defer techniqueHeadersMutex.Unlock()
	executedTechniques[technique] = true
}

func markTechniqueProduced(technique string) {
	techniqueHeadersMutex.Lock()
	defer techniqueHeadersMutex.Unlock()
	producedTechniques[technique] = true
	switch technique {
	case "headers-ip", "headers-simple", "headers-host":
		producedTechniques["headers"] = true
	}
}

func recordFinding(result Result) {
	if result.defaultReq {
		return
	}
	topFindingsMutex.Lock()
	defer topFindingsMutex.Unlock()
	topFindings = append(topFindings, result)
}

func printTopFindings(limit int) {
	if getVerbose() || limit <= 0 {
		return
	}
	topFindingsMutex.Lock()
	defer topFindingsMutex.Unlock()
	if len(topFindings) == 0 {
		return
	}
	sort.SliceStable(topFindings, func(i, j int) bool {
		if topFindings[i].score == topFindings[j].score {
			return topFindings[i].contentLength > topFindings[j].contentLength
		}
		return topFindings[i].score > topFindings[j].score
	})
	if limit > len(topFindings) {
		limit = len(topFindings)
	}
	likely := make([]Result, 0, limit)
	variations := make([]Result, 0, limit)
	for _, finding := range topFindings {
		if finding.score >= topScoreMin {
			likely = append(likely, finding)
		} else if finding.score >= variationScoreMin {
			variations = append(variations, finding)
		}
		if len(likely) == limit && len(variations) == limit {
			break
		}
	}
	if len(likely) == 0 && len(variations) == 0 {
		return
	}
	if len(likely) > 0 {
		for i := range likely {
			likely[i] = revalidateResult(likely[i], 2)
		}
		fmt.Println(color.MagentaString("\n━━━━━━━━━━━━━━ LIKELY BYPASS ━━━━━━━━━━━━━━━━━"))
		printFindingGroup(likely, limit, true)
	}
	if len(variations) > 0 {
		for i := range variations {
			variations[i] = revalidateResult(variations[i], 1)
		}
		fmt.Println(color.MagentaString("\n━━━━━━━━━━━ INTERESTING VARIATIONS ━━━━━━━━━━━"))
		printFindingGroup(variations, limit, true)
	}
}

func printFindingGroup(findings []Result, limit int, includeCurl bool) {
	collapsed := collapseFindingFamilies(findings)
	if limit > len(collapsed) {
		limit = len(collapsed)
	}
	for i, f := range collapsed[:limit] {
		scoreColor := color.New(color.FgHiBlack)
		techColor := color.New(color.FgWhite, color.Bold)
		marker := " "
		switch f.likelihood {
		case "high":
			scoreColor = color.New(color.FgGreen, color.Bold)
			techColor = color.New(color.FgGreen, color.Bold)
			marker = "!"
		case "medium":
			scoreColor = color.New(color.FgYellow, color.Bold)
			techColor = color.New(color.FgYellow, color.Bold)
			marker = "+"
		}
		label := f.technique
		if pretty, ok := techniqueLabels[f.technique]; ok {
			label = pretty
		}
		fmt.Printf("%-12s %-22s %-9s %8s\n",
			scoreColor.Sprintf("[%s%2d %s]", marker, f.score, strings.ToUpper(f.likelihood)),
			techColor.Sprintf("%s", label),
			colorizeStatusTransition(f),
			color.BlueString(strconv.Itoa(f.contentLength)+"b"),
		)
		if f.relatedCount > 0 {
			printWrappedBlock("      similar: ", fmt.Sprintf("+%d similar results in same family", f.relatedCount))
		}
		printWrappedBlock("         why: ", colorizeWhyText(f.scoreReason))
		printWrappedBlock("        item: ", f.line)
		if includeCurl && f.reproCurl != "" {
			printWrappedCommandBlock("        curl: ", f.reproCurl)
		}
		if i < limit-1 {
			fmt.Println()
		}
	}
}

func collapseFindingFamilies(findings []Result) []Result {
	seen := make(map[string]int)
	var collapsed []Result
	for _, finding := range findings {
		key := finding.familyKey
		if key == "" {
			key = strconv.Itoa(finding.statusCode) + "|" + strconv.Itoa(finding.contentLength)
		}
		key = finding.technique + "|" + key
		if idx, ok := seen[key]; ok {
			collapsed[idx].relatedCount++
			continue
		}
		seen[key] = len(collapsed)
		collapsed = append(collapsed, finding)
	}
	return collapsed
}

func printWrappedBlock(prefix string, text string) {
	width := terminalWidth()
	if width < 60 {
		width = 60
	}
	available := width - len(prefix)
	if available < 20 {
		available = 20
	}
	lines := wrapText(text, available)
	for i, line := range lines {
		if i == 0 {
			fmt.Printf("%s%s\n", prefix, line)
			continue
		}
		fmt.Printf("%s%s\n", strings.Repeat(" ", len(prefix)), line)
	}
}

func colorizeWhyText(text string) string {
	replacer := strings.NewReplacer(
		"status", color.YellowString("status"),
		"body changed", color.GreenString("body changed"),
		"location changed", color.CyanString("location changed"),
		"redirect anomaly", color.CyanString("redirect anomaly"),
		"type changed", color.MagentaString("type changed"),
		"server changed", color.HiBlackString("server changed"),
		"len Δ", color.BlueString("len Δ"),
		"unstable replay", color.MagentaString("unstable replay"),
		"minor variation", color.HiBlackString("minor variation"),
	)
	return replacer.Replace(text)
}

func printWrappedCommandBlock(prefix string, text string) {
	lines := splitCommandForDisplay(text, terminalWidth()-len(prefix))
	for i, line := range lines {
		suffix := ""
		if i < len(lines)-1 {
			suffix = " \\"
		}
		if i == 0 {
			fmt.Printf("%s%s%s\n", prefix, line, suffix)
		} else {
			fmt.Printf("%s%s%s\n", strings.Repeat(" ", len(prefix)), line, suffix)
		}
	}
}

func splitCommandForDisplay(text string, width int) []string {
	if width < 30 {
		width = 30
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return []string{text}
	}
	var lines []string
	current := parts[0]
	for _, part := range parts[1:] {
		next := current + " " + part
		if len(next) > width && current != "" {
			lines = append(lines, current)
			current = part
			continue
		}
		current = next
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func truncateForDisplay(text string, width int) string {
	if width <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func printSilentTechniqueSummary() {
	if getVerbose() {
		return
	}
	techniqueHeadersMutex.Lock()
	defer techniqueHeadersMutex.Unlock()

	var silent []string
	for tech := range executedTechniques {
		if tech == "default" {
			continue
		}
		if !producedTechniques[tech] {
			if label, ok := techniqueLabels[tech]; ok {
				silent = append(silent, label)
			} else {
				silent = append(silent, tech)
			}
		}
	}
	if len(silent) == 0 {
		return
	}
	fmt.Printf("\n%s %s\n",
		color.New(color.FgHiBlack, color.Bold).Sprint("no visible results:"),
		color.New(color.FgHiBlack).Sprintf("%d techniques", len(silent)),
	)
}

func revalidateResult(result Result, attempts int) Result {
	if result.replay == nil || attempts <= 0 {
		return result
	}

	matches := 0
	for i := 0; i < attempts; i++ {
		resp, err := executeReplay(*result.replay)
		if err != nil {
			continue
		}
		if resp.statusCode == result.statusCode &&
			resp.contentLength == result.contentLength &&
			(resp.bodyHash == "" || result.bodyHash == "" || resp.bodyHash == result.bodyHash) {
			matches++
		}
	}
	result.revalidated = attempts
	result.replayMatches = matches
	if attempts > 0 && matches < attempts/2+1 {
		result.score -= 10
		if result.score < 0 {
			result.score = 0
		}
		result.likelihood = classifyLikelihood(result.score)
		result.scoreReason += ", unstable replay"
	}
	return result
}

func executeReplay(spec ReplaySpec) (ResponseInfo, error) {
	switch spec.kind {
	case "http":
		var proxyURL *url.URL
		if spec.proxy != "" {
			parsed, err := url.Parse(spec.proxy)
			if err != nil {
				return ResponseInfo{}, err
			}
			proxyURL = parsed
		}
		return requestWithRetryBody(spec.method, spec.uri, spec.headers, spec.body, proxyURL, false, spec.timeout, spec.redirect)
	case "raw":
		return rawRequest(spec.method, spec.uri, spec.requestTarget, spec.headers, spec.body, spec.timeout)
	case "curl":
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(spec.timeout)*time.Millisecond)
		defer cancel()
		cmd := exec.CommandContext(ctx, "curl", spec.curlArgs...)
		out, err := cmd.Output()
		if err != nil {
			return ResponseInfo{}, err
		}
		res := parseCurlOutput(string(out), "curl-replay")
		return ResponseInfo{
			statusCode:    res.statusCode,
			contentLength: res.contentLength,
			bodyHash:      res.bodyHash,
			location:      res.location,
			contentType:   res.contentType,
			server:        res.server,
		}, nil
	default:
		return ResponseInfo{}, fmt.Errorf("unsupported replay kind %q", spec.kind)
	}
}

func wrapText(text string, width int) []string {
	if len(text) <= width {
		return []string{text}
	}

	var lines []string
	remaining := text
	for len(remaining) > width {
		split := strings.LastIndex(remaining[:width], " ")
		if split <= 0 {
			split = width
		}
		lines = append(lines, strings.TrimSpace(remaining[:split]))
		remaining = strings.TrimSpace(remaining[split:])
	}
	if remaining != "" {
		lines = append(lines, remaining)
	}
	return lines
}

func terminalWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 100
}

// printSuppressedSummary prints a summary of suppressed results for a technique.
// Should be called after a technique finishes.
func printSuppressedSummary(technique string) {
	return
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

func colorizeStatusCodeLabel(status int, label string) string {
	switch {
	case status >= 200 && status < 300:
		return color.GreenString(label)
	case status >= 300 && status < 400:
		return color.YellowString(label)
	case status == 400 || status == 401 || status == 403 || status == 404:
		return color.RedString(label)
	case status == 405 || status == 406 || status == 407 || status == 408 || status == 413 || status == 429:
		return color.New(color.FgHiMagenta).Sprint(label)
	case status >= 400 && status < 500:
		return color.New(color.FgMagenta).Sprint(label)
	case status >= 500:
		return color.MagentaString(label)
	default:
		return label
	}
}

func colorizeContentLengthLabel(cl int, label string) string {
	defCl := getDefaultRespCl()

	if defCl == 0 || cl == 0 {
		return color.BlueString(label)
	}

	ratio := float64(cl) / float64(defCl)
	switch {
	case ratio > 2.0:
		return color.GreenString(label)
	case ratio > 1.3:
		return color.YellowString(label)
	case ratio < 0.7:
		return color.YellowString(label)
	default:
		return color.BlueString(label)
	}
}

// printResult prints the result of an HTTP request in a tabular format with colored output based on the status codes.
// Caller must hold printMutex.
func printResult(result Result) {
	if result.statusCode == 0 {
		return
	}

	// Clear progress bar, print result, redraw progress bar
	clearActiveProgress()
	ensureResultsSectionLocked(result.defaultReq)
	fmt.Println(formatPrintedResult(result))
	if getVerbose() && result.reproCurl != "" {
		fmt.Printf("    curl: %s\n", result.reproCurl)
	}
	redrawActiveProgress()
}

func ensureResultsSectionLocked(defaultReq bool) {
	if defaultReq {
		if printedBaselineHeader {
			return
		}
		fmt.Println()
		fmt.Println(color.New(color.FgWhite, color.Bold).Sprint("BASELINE"))
		printedBaselineHeader = true
		return
	}
	if printedFindingsHeader {
		return
	}
	fmt.Println()
	fmt.Println(color.New(color.FgWhite, color.Bold).Sprint("FINDINGS"))
	printedFindingsHeader = true
}

func formatResultsRow(techCol, statusCol, sizeCol, item string) string {
	return fmt.Sprintf("  %s %s %s %s  %s", techCol, scoreColPlaceholder(), statusCol, sizeCol, item)
}

func formatPrintedResult(result Result) string {
	const (
		techWidth  = 10
		scoreWidth = 4
		codeWidth  = 4
		sizeWidth  = 11
	)
	techLabel := result.technique
	if alias, ok := techniqueAliases[result.technique]; ok {
		techLabel = alias
	}
	techStyle := color.New(color.FgWhite)
	baselineStatus := baselineForTechnique(result.technique).statusCode
	if baselineStatus != 0 && baselineStatus != result.statusCode {
		techStyle = color.New(color.FgWhite, color.Bold)
	}
	techCol := techStyle.Sprintf("%-*s", techWidth, techLabel)
	statusLabel := colorizeStatusCodeLabel(result.statusCode, fmt.Sprintf("%-*s", codeWidth, strconv.Itoa(result.statusCode)))
	clStr := colorizeContentLengthLabel(result.contentLength, fmt.Sprintf("%*s", sizeWidth, strconv.Itoa(result.contentLength)+" bytes"))
	itemWidth := terminalWidth() - 2 - techWidth - 1 - scoreWidth - 1 - codeWidth - 1 - sizeWidth - 2
	item := truncateForDisplay(result.line, itemWidth)
	if result.defaultReq {
		techDefault := color.New(color.FgHiBlack, color.Bold).Sprintf("%-*s", techWidth, "default")
		scoreBlank := strings.Repeat(" ", scoreWidth)
		return fmt.Sprintf("  %s %s %s %s    %s", techDefault, scoreBlank, statusLabel, clStr, item)
	}

	scoreCol := formatCompactScore(result.score, result.likelihood)
	return fmt.Sprintf("  %s %s %s %s    %s", techCol, scoreCol, statusLabel, clStr, item)
}

func scoreColPlaceholder() string {
	return fmt.Sprintf("%-4s", "")
}

func formatCompactScore(score int, likelihood string) string {
	marker := "."
	style := color.New(color.FgHiBlack)
	switch likelihood {
	case "medium":
		marker = "+"
		style = color.New(color.FgYellow, color.Bold)
	case "high":
		marker = "!"
		style = color.New(color.FgGreen, color.Bold)
	}
	return style.Sprintf("%-4s", fmt.Sprintf("%d%s", score, marker))
}

// showInfo prints the configuration options used for the scan in a compact two-column layout.
func showInfo(options RequestOptions) {
	if nobanner {
		return
	}

	if options.verbose {
		dim := color.New(color.Faint).SprintFunc()
		val := color.New(color.FgWhite, color.Bold).SprintFunc()
		printRow := func(label, value string) {
			prefix := fmt.Sprintf("  %-13s ", label+":")
			width := terminalWidth() - len(prefix)
			if width < 20 {
				width = 20
			}
			lines := wrapText(value, width)
			for i, line := range lines {
				if i == 0 {
					fmt.Printf("%s%s\n", dim(prefix), val(line))
					continue
				}
				fmt.Printf("%s%s\n", strings.Repeat(" ", len(prefix)), val(line))
			}
		}

		fmt.Println(color.MagentaString("━━━━━━━━━━━━━━━━━━━━━━ NOMORE403 ━━━━━━━━━━━━━━━━━━━━━━━"))
		printRow("Target", options.uri)
		printRow("Method", options.method)
		printRow("Timeout", fmt.Sprintf("%dms", options.timeout))
		printRow("Delay", fmt.Sprintf("%dms", delay))
		printRow("User-Agent", options.userAgent)

		proxyStr := "-"
		if len(options.proxy.Host) != 0 {
			proxyStr = options.proxy.Host
		}
		ipStr := "-"
		if len(options.bypassIP) != 0 {
			ipStr = options.bypassIP
		}
		printRow("Proxy", proxyStr)
		printRow("Bypass IP", ipStr)

		var flags []string
		if options.redirect {
			flags = append(flags, "redirects")
		}
		if options.rateLimit {
			flags = append(flags, "rate-limit")
		}
		if !options.autocalibrate {
			flags = append(flags, "no-calibrate")
		}
		if options.strictCalibrate {
			flags = append(flags, "strict-calibrate")
		}
		if options.rawHTTP {
			flags = append(flags, "raw-http")
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
		printRow("Flags", flagStr)

		if len(options.frontendHints) > 0 {
			printRow("Frontend", strings.Join(options.frontendHints, ", "))
		}

		if len(options.reqHeaders) > 0 && options.reqHeaders[0] != "" {
			hdrs := make([]string, 0)
			for _, h := range options.headers {
				if h.key != "User-Agent" {
					hdrs = append(hdrs, h.key+": "+h.value)
				}
			}
			if len(hdrs) > 0 {
				printRow("Headers", strings.Join(hdrs, " | "))
			}
		}

		if len(statusCodes) > 0 {
			printRow("Status", strings.Join(statusCodes, ", "))
		}
		printRow("Techniques", strings.Join(options.techniques, ", "))
		printRow("Payloads", options.folder)
		return
	}

	targetWidth := terminalWidth() - 48
	if targetWidth < 36 {
		targetWidth = 36
	}
	labelStyle := color.New(color.FgHiBlack, color.Bold).SprintFunc()
	valueStyle := color.New(color.FgWhite, color.Bold).SprintFunc()
	meta := []string{
		labelStyle("target:") + " " + valueStyle(truncateForDisplay(options.uri, targetWidth)),
		labelStyle("method:") + " " + valueStyle(options.method),
	}
	if len(options.frontendHints) > 0 {
		meta = append(meta, labelStyle("frontend:")+" "+valueStyle(strings.Join(options.frontendHints, ", ")))
	}
	meta = append(meta, labelStyle("payloads:")+" "+valueStyle(options.folder))
	fmt.Println(strings.Join(meta, "   "))
}

// generateCaseCombinations generates all combinations of uppercase and lowercase letters for a given string.
func generateCaseCombinations(s string) []string {
	runes := []rune(s)
	if len(runes) == 0 {
		return []string{""}
	}
	return generateCaseCombinationsRunes(runes)
}

func generateCaseCombinationsRunes(runes []rune) []string {
	if len(runes) == 0 {
		return []string{""}
	}

	first := runes[0]
	var firstCharCombinations []string
	lower := unicode.ToLower(first)
	upper := unicode.ToUpper(first)
	if unicode.IsLetter(first) && lower != upper {
		firstCharCombinations = []string{string(lower), string(upper)}
	} else {
		firstCharCombinations = []string{string(first)}
	}

	subCombinations := generateCaseCombinationsRunes(runes[1:])
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
	resp, err := requestWithRetry(options.method, options.uri, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
	if err != nil {
		if errors.Is(err, ErrRateLimited) {
			log.Printf("[!] Rate limited on default request, aborting")
			return
		}
		log.Println(err)
	}

	// Capture the default response signature for smart filtering
	setDefaultSc(resp.statusCode)
	setDefaultRespCl(resp.contentLength)
	if !options.autocalibrate || getDefaultCl() == 0 {
		setDefaultCl(resp.contentLength)
	}

	base := resultFromResponse(options.uri, true, "default", resp)
	attachHTTPReplay(&base, options.method, options.uri, options.headers, "", options.redirect, options.proxy, options.timeout)
	setGlobalBaseline(resp)
	setTechniqueBaseline("default", resp)
	printResponse(base, "default")
}

// requestMethods makes HTTP requests using a list of methods from a file and prints the results.
func requestMethods(options RequestOptions) {
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
			resp, err := requestWithRetry(line, options.uri, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					log.Printf("[!] Rate limited, stopping verb tampering")
					return
				}
				logVerbose(err)
				return
			}

			contentLength := resp.contentLength

			verbTamperingResultsMutex.Lock()
			verbTamperingResults[line] = contentLength
			verbTamperingResultsMutex.Unlock()

			if isCalibrationMatch(contentLength) {
				return
			}

			result := resultFromResponse(line, false, "verb-tampering", resp)
			attachHTTPReplay(&result, line, options.uri, options.headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "verb-tampering")
		}(line)
	}
	w.WaitAllDone()
	p.finish()
}

// requestMethodsCaseSwitching makes HTTP requests using a list of methods from a file and prints the results.
func requestMethodsCaseSwitching(options RequestOptions) {
	lines, err := parseFile(options.folder + "/httpmethods")
	if err != nil {
		log.Printf("[!] Skipping verb case switching: %v", err)
		return
	}

	// Pre-build all work items to know the total for progress.
	type workItem struct {
		method                string
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
		selectedCombinations := selectRandomCombinations(filteredCombinations, 10)
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
			resp, err := requestWithRetry(item.method, options.uri, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			contentLength := resp.contentLength

			if contentLength == item.originalContentLength || isCalibrationMatch(contentLength) {
				return
			}

			result := resultFromResponse(item.method, false, "verb-tampering-case", resp)
			attachHTTPReplay(&result, item.method, options.uri, options.headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "verb-tampering-case")
		}(item)
	}
	w.WaitAllDone()
	p.finish()
}

// requestHeaders makes HTTP requests using a list of headers from a file and prints the results.
func requestHeaders(options RequestOptions) {
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

	// Generate Host header variations: port confusion, trailing dot FQDN, and case switching.
	// These are NOT covered by the header+IP combinations above (which test Host: <ip>).
	var hostVariations []header
	parsedURL, parseErr := url.Parse(options.uri)
	if parseErr == nil {
		originalHost := parsedURL.Hostname()
		if len(originalHost) > 0 {
			hostVariations = append(hostVariations,
				header{"Host", originalHost + ":80"},
				header{"Host", originalHost + ":443"},
				header{"Host", originalHost + ":8080"},
				header{"Host", originalHost + ":8443"},
				header{"Host", originalHost + "."},                                   // trailing dot FQDN
				header{"Host", strings.ToUpper(originalHost)},                        // full uppercase
				header{"Host", strings.ToUpper(originalHost[:1]) + originalHost[1:]}, // first char uppercase
			)
		}
	}

	totalRequests := len(combined) + len(simpleHeaders) + len(hostVariations)
	if totalRequests == 0 {
		return
	}
	setTechniqueBaselines(globalBaseline(), "headers-ip", "headers-simple", "headers-host")

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

			resp, err := requestWithRetry(options.method, options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			result := resultFromResponse(item.Line+": "+item.IP, false, "headers-ip", resp)
			attachHTTPReplay(&result, options.method, options.uri, headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "headers-ip")
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

			resp, err := requestWithRetry(options.method, options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			result := resultFromResponse(line, false, "headers-simple", resp)
			attachHTTPReplay(&result, options.method, options.uri, headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "headers-simple")
		}(simpleHeader)
	}

	// Process Host header variations (port confusion, trailing dot, case switching)
	for _, v := range hostVariations {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(v header) {
			defer w.Done()
			defer p.done()

			headers := make([]header, len(options.headers))
			copy(headers, options.headers)
			headers = append(headers, v)

			resp, err := requestWithRetry(options.method, options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			if isCalibrationMatch(resp.contentLength) {
				return
			}
			result := resultFromResponse(v.key+": "+v.value, false, "headers-host", resp)
			attachHTTPReplay(&result, options.method, options.uri, headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "headers-host")
		}(v)
	}
	w.WaitAllDone()
	p.finish()
	printSuppressedSummary("headers-ip")
	printSuppressedSummary("headers-simple")
	printSuppressedSummary("headers-host")
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

			resp, err := requestWithRetry(options.method, joinURL(options.uri, line), options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			contentLength := resp.contentLength

			if isCalibrationMatch(contentLength) {
				return
			}

			result := resultFromResponse(joinURL(options.uri, line), false, "endpaths", resp)
			attachHTTPReplay(&result, options.method, joinURL(options.uri, line), options.headers, "", options.redirect, options.proxy, options.timeout)
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

			resp, err := requestWithRetry(options.method, fullpath, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			if isCalibrationMatch(resp.contentLength) {
				return
			}
			result := resultFromResponse(fullpath, false, "midpaths", resp)
			attachHTTPReplay(&result, options.method, fullpath, options.headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "midpaths")
		}(line)
	}
	w.WaitAllDone()
	p.finish()
}

// doubleEncode returns the double-URL-encoded form of a character.
// e.g., 's' (0x73) → %73 → %2573
func doubleEncode(c rune) string {
	singleEncoded := fmt.Sprintf("%%%X", c)
	return url.QueryEscape(singleEncoded)
}

// requestDoubleEncoding makes HTTP requests doing a double URL encode of the path.
// It tests three strategies:
//  1. Per-character: double-encode each character individually (e.g., /admi%256E)
//  2. Last char only: double-encode the last char of the final path segment
//     (matches the Rhynorater/DEF CON 32 technique: /getAllUser%2573)
//  3. Full segment: double-encode every char in the last path segment at once
//     (e.g., /%2561%2564%256D%2569%256E)
func requestDoubleEncoding(options RequestOptions) {
	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}

	originalPath := parsedURL.Path
	if len(originalPath) == 0 || originalPath == "/" {
		return
	}

	baseURL := parsedURL.Scheme + "://" + parsedURL.Host
	query := ""
	if parsedURL.RawQuery != "" {
		query = "?" + parsedURL.RawQuery
	}

	var payloads []string

	// Strategy 1: per-character double encoding
	for i, c := range originalPath {
		if c == '/' {
			continue
		}
		modifiedPath := originalPath[:i] + doubleEncode(c) + originalPath[i+len(string(c)):]
		payloads = append(payloads, baseURL+modifiedPath+query)
	}

	// Strategy 2 & 3: last-char and full-segment double encoding
	segments := strings.Split(strings.Trim(originalPath, "/"), "/")
	if len(segments) > 0 {
		lastSeg := segments[len(segments)-1]
		basePath := "/"
		if len(segments) > 1 {
			basePath = "/" + strings.Join(segments[:len(segments)-1], "/") + "/"
		}

		// Strategy 2: double-encode only the last character of the last segment
		if len(lastSeg) > 0 {
			runes := []rune(lastSeg)
			lastChar := runes[len(runes)-1]
			modified := basePath + string(runes[:len(runes)-1]) + doubleEncode(lastChar)
			if strings.HasSuffix(originalPath, "/") {
				modified += "/"
			}
			payloads = append(payloads, baseURL+modified+query)
		}

		// Strategy 3: double-encode every character in the last segment
		if len(lastSeg) > 1 {
			var fullEncoded strings.Builder
			fullEncoded.WriteString(basePath)
			for _, c := range lastSeg {
				fullEncoded.WriteString(doubleEncode(c))
			}
			if strings.HasSuffix(originalPath, "/") {
				fullEncoded.WriteString("/")
			}
			payloads = append(payloads, baseURL+fullEncoded.String()+query)
		}
	}

	// Deduplicate payloads (Strategy 2 last-char overlaps with last per-char payload)
	payloads = removeDuplicates(payloads)

	if len(payloads) == 0 {
		return
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("double-encoding", len(payloads))

	for _, payload := range payloads {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(payload string) {
			defer w.Done()
			defer p.done()

			resp, err := requestWithRetry(options.method, payload, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			result := resultFromResponse(payload, false, "double-encoding", resp)
			attachHTTPReplay(&result, options.method, payload, options.headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "double-encoding")
		}(payload)
	}

	w.WaitAllDone()
	p.finish()
}

// requestUnicodeEncoding makes HTTP requests using Unicode/overlong UTF-8 encoded paths
func requestUnicodeEncoding(options RequestOptions) {
	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}

	originalPath := parsedURL.Path
	if len(originalPath) == 0 || originalPath == "/" {
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

			resp, err := requestWithRetry(options.method, payload, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			if isCalibrationMatch(resp.contentLength) {
				return
			}
			result := resultFromResponse(payload, false, "unicode-encoding", resp)
			attachHTTPReplay(&result, options.method, payload, options.headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "unicode-encoding")
		}(payload)
	}

	w.WaitAllDone()
	p.finish()
}

// requestPayloadPositions makes HTTP requests injecting payloads at custom marked positions.
func requestPayloadPositions(options RequestOptions) {
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

			resp, err := requestWithRetry(options.method, item.uri, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			if isCalibrationMatch(resp.contentLength) {
				return
			}
			result := resultFromResponse(item.uri, false, "payload-position", resp)
			attachHTTPReplay(&result, options.method, item.uri, options.headers, "", options.redirect, options.proxy, options.timeout)
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
	if !isCurlAvailable() {
		log.Printf("[!] Skipping HTTP versions technique: curl not found in PATH")
		return
	}

	httpVersions := []string{"--http1.0", "--http2"}
	proxyValue := ""
	if options.proxy != nil {
		proxyValue = options.proxy.String()
	}

	baselineArgs, _ := buildCurlVersionInvocation(options.method, options.uri, options.headers, proxyValue, "--http1.1", options.redirect)
	baselineResult := curlRequest(baselineArgs, "--http1.1", options.timeout)
	if baselineResult.statusCode != 0 {
		setTechniqueBaseline("http-versions", responseInfoFromResult(baselineResult))
	}

	for _, version := range httpVersions {
		args, display := buildCurlVersionInvocation(options.method, options.uri, options.headers, proxyValue, version, options.redirect)
		res := curlRequest(args, version, options.timeout)
		attachCurlReplay(&res, args, display, options.timeout)
		printResponse(res, "http-versions")
	}
}

func requestHTTPParser(options RequestOptions) {
	if !isCurlAvailable() {
		log.Printf("[!] Skipping HTTP parser technique: curl not found in PATH")
		return
	}

	proxyValue := ""
	if options.proxy != nil {
		proxyValue = options.proxy.String()
	}

	baselineArgs, _ := buildCurlVersionInvocation(options.method, options.uri, options.headers, proxyValue, "--http1.1", options.redirect)
	baselineResult := curlRequest(baselineArgs, "--http1.1", options.timeout)
	if baselineResult.statusCode != 0 {
		setTechniqueBaseline("http-parser", responseInfoFromResult(baselineResult))
	}

	args, display := buildCurlParserInvocation(options.method, options.uri, proxyValue, options.redirect)
	res := curlRequest(args, "http-parser", options.timeout)
	if res.statusCode == 0 {
		return
	}
	res.line = "minimal curl request"
	attachCurlReplay(&res, args, display, options.timeout)
	printResponse(res, "http-parser")
}

func buildCurlVersionInvocation(method, uri string, headers []header, proxy string, httpVersion string, followRedirects bool) ([]string, string) {
	args := []string{"-i", "-s", httpVersion}
	displayArgs := []string{"curl", "-i", "-s", httpVersion}
	if method != "" && method != "GET" {
		args = append(args, "-X", method)
		displayArgs = append(displayArgs, "-X", shellQuote(method))
	}
	for _, h := range headers {
		args = append(args, "-H", h.key+": "+h.value)
		displayArgs = append(displayArgs, "-H", shellQuote(h.key+": "+h.value))
	}
	if proxy != "" {
		args = append(args, "-x", proxy)
		displayArgs = append(displayArgs, "-x", shellQuote(proxy))
	}
	if followRedirects {
		args = append(args, "-L")
		displayArgs = append(displayArgs, "-L")
	}
	args = append(args, "--insecure")
	displayArgs = append(displayArgs, "--insecure")
	args = append(args, uri)
	displayArgs = append(displayArgs, shellQuote(uri))
	return args, strings.Join(displayArgs, " ")
}

func buildCurlParserInvocation(method, uri string, proxy string, followRedirects bool) ([]string, string) {
	args := []string{"-i", "-s", "--http1.1"}
	displayArgs := []string{"curl", "-i", "-s", "--http1.1"}
	if method != "" && method != "GET" {
		args = append(args, "-X", method)
		displayArgs = append(displayArgs, "-X", shellQuote(method))
	}
	for _, headerName := range []string{"User-Agent", "Accept", "Connection", "Host"} {
		args = append(args, "-H", headerName+":")
		displayArgs = append(displayArgs, "-H", shellQuote(headerName+":"))
	}
	if proxy != "" {
		args = append(args, "-x", proxy)
		displayArgs = append(displayArgs, "-x", shellQuote(proxy))
	}
	if followRedirects {
		args = append(args, "-L")
		displayArgs = append(displayArgs, "-L")
	}
	args = append(args, "--insecure", uri)
	displayArgs = append(displayArgs, "--insecure", shellQuote(uri))
	return args, strings.Join(displayArgs, " ")
}

func curlRequest(args []string, httpVersion string, timeout int) Result {
	if len(args) == 0 {
		return Result{}
	}

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

	serverResponse := output
	if blocks := strings.Split(output, "\r\n\r\n"); len(blocks) > 1 {
		for i, block := range blocks {
			if strings.Contains(strings.ToLower(block), "connection established") && i+1 < len(blocks) {
				serverResponse = strings.Join(blocks[i+1:], "\r\n\r\n")
				break
			}
		}
	}

	if serverResponse == "" {
		log.Println("[!] No valid HTTP server response found in curl output")
		return Result{}
	}

	headerBodyParts := strings.SplitN(serverResponse, "\r\n\r\n", 2)
	if len(headerBodyParts) == 0 || headerBodyParts[0] == "" {
		log.Printf("[!] Invalid server response format: %s", serverResponse)
		return Result{}
	}

	headerBlock := headerBodyParts[0]
	body := ""
	if len(headerBodyParts) == 2 {
		body = headerBodyParts[1]
	}

	lines := strings.Split(headerBlock, "\r\n")
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

	bodyHash := ""
	bodySize := len(body)
	location := ""
	contentType := ""
	server := ""
	for _, headerLine := range lines[1:] {
		pair := strings.SplitN(headerLine, ":", 2)
		if len(pair) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(pair[0]))
		value := strings.TrimSpace(pair[1])
		switch key {
		case "location":
			location = value
		case "content-type":
			contentType = value
		case "server":
			server = value
		}
	}
	if body != "" {
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(body))
		bodyHash = fmt.Sprintf("%x", hasher.Sum64())
	}

	return Result{
		line:          httpVersionOutput,
		statusCode:    statusCode,
		contentLength: bodySize,
		defaultReq:    false,
		bodyHash:      bodyHash,
		location:      location,
		contentType:   contentType,
		server:        server,
	}
}

// requestPathCaseSwitching makes HTTP requests by capitalizing each letter in the last part of the URI.
func requestPathCaseSwitching(options RequestOptions) {
	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}

	baseuri := parsedURL.Scheme + "://" + parsedURL.Host
	escapedPath := parsedURL.EscapedPath()
	if escapedPath == "" {
		escapedPath = parsedURL.Path
	}
	queryStr := ""
	if parsedURL.RawQuery != "" {
		queryStr = "?" + parsedURL.RawQuery
	}

	if len(escapedPath) == 0 || escapedPath == "/" {
		return
	}

	segments := strings.Split(escapedPath, "/")
	lastSegmentIndex := -1
	for i := len(segments) - 1; i >= 0; i-- {
		if segments[i] != "" {
			lastSegmentIndex = i
			break
		}
	}
	if lastSegmentIndex == -1 {
		return
	}

	originalSegment := segments[lastSegmentIndex]
	pathCombinations := filterOriginalMethod(originalSegment, generateCaseCombinations(originalSegment))
	selectedPaths := selectRandomCombinations(pathCombinations, 20)
	if len(selectedPaths) == 0 {
		return
	}
	w := goccm.New(maxGoroutines)
	p := newProgress("path-case-switching", len(selectedPaths))

	for _, segmentVariant := range selectedPaths {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(segmentVariant string) {
			defer w.Done()
			defer p.done()

			pathSegments := append([]string(nil), segments...)
			pathSegments[lastSegmentIndex] = segmentVariant
			fullpath := baseuri + strings.Join(pathSegments, "/") + queryStr

			resp, err := requestWithRetry(options.method, fullpath, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			result := resultFromResponse(fullpath, false, "path-case-switching", resp)
			attachHTTPReplay(&result, options.method, fullpath, options.headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "path-case-switching")
		}(segmentVariant)
	}

	w.WaitAllDone()
	p.finish()
}

// requestHopByHop tests hop-by-hop header abuse.
// By listing security-relevant headers in the Connection header, some proxies
// strip them before forwarding, potentially bypassing authentication/authorization.
func requestHopByHop(options RequestOptions) {
	// Headers that, if stripped by a proxy via hop-by-hop, could bypass security checks.
	// We send the target header with a bypass value AND list it in Connection so a
	// compliant proxy strips it before forwarding to the backend.
	bypassIP := options.bypassIP
	if bypassIP == "" {
		bypassIP = "127.0.0.1"
	}

	hopByHopTargets := []struct {
		name  string
		value string
	}{
		{"X-Forwarded-For", bypassIP},
		{"X-Real-IP", bypassIP},
		{"X-Forwarded-Host", "localhost"},
		{"X-Custom-IP-Authorization", bypassIP},
		{"X-Original-URL", "/"},
		{"X-Rewrite-URL", "/"},
		{"CF-Connecting-IP", bypassIP},
		{"True-Client-IP", bypassIP},
	}

	if len(hopByHopTargets) == 0 {
		return
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("hop-by-hop", len(hopByHopTargets))

	for _, target := range hopByHopTargets {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(target struct {
			name  string
			value string
		}) {
			defer w.Done()
			defer p.done()

			headers := make([]header, len(options.headers))
			copy(headers, options.headers)
			headers = append(headers, header{target.name, target.value})
			headers = append(headers, header{"Connection", "close, " + target.name})

			resp, err := requestWithRetry(options.method, options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			if isCalibrationMatch(resp.contentLength) {
				return
			}
			result := resultFromResponse(target.name+": "+target.value+" + Connection: close, "+target.name, false, "hop-by-hop", resp)
			attachHTTPReplay(&result, options.method, options.uri, headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "hop-by-hop")
		}(target)
	}
	w.WaitAllDone()
	p.finish()
}

// requestAbsoluteURI tests sending absolute URI in the request line.
// Some proxies parse the path differently when given an absolute URI vs a relative one.
func requestAbsoluteURI(options RequestOptions) {
	if !isCurlAvailable() {
		log.Printf("[!] Skipping absolute URI technique: curl not found in PATH")
		return
	}

	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}

	// Preserve the original path+query exactly as provided (keeps encoding and query string).
	pathAndQuery := parsedURL.RequestURI() // e.g., /admin?foo=bar

	// Build payloads: absolute URI forms that might confuse proxies.
	// Each label shows the curl command to reproduce.
	payloads := buildAbsoluteURIPayloads(parsedURL, pathAndQuery)

	proxyValue := ""
	if options.proxy != nil {
		proxyValue = options.proxy.String()
	}

	connectURL := parsedURL.Scheme + "://" + parsedURL.Host + pathAndQuery

	for _, requestTarget := range payloads {
		args := []string{"-i", "-s", "--request-target", requestTarget}
		if proxyValue != "" {
			args = append(args, "-x", proxyValue)
		}
		if options.redirect {
			args = append(args, "-L")
		}
		args = append(args, "--insecure")
		// Use the original host for the actual connection
		args = append(args, connectURL)

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(options.timeout)*time.Millisecond)
		cmd := exec.CommandContext(ctx, "curl", args...)
		out, err := cmd.Output()
		cancel()
		if err != nil {
			logVerbose("[!] Absolute URI curl failed:", err)
			continue
		}

		// Show the curl command so the user can reproduce the bypass
		label := "curl --request-target '" + requestTarget + "' " + connectURL
		res := parseCurlOutput(string(out), label)
		if res.statusCode == 0 {
			continue
		}
		res.line = "request-target: " + requestTarget
		attachCurlReplay(&res, args, label, options.timeout)
		printResponse(res, "absolute-uri")
	}
}

func buildAbsoluteURIPayloads(parsedURL *url.URL, pathAndQuery string) []string {
	candidates := []string{
		parsedURL.Scheme + "://" + parsedURL.Host + pathAndQuery,
		parsedURL.Scheme + "://anything@" + parsedURL.Host + pathAndQuery,
	}
	seen := make(map[string]bool, len(candidates))
	payloads := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		payloads = append(payloads, candidate)
	}
	return payloads
}

func setRawTechniqueBaseline(technique string, options RequestOptions, requestTarget string) {
	baselineHeaders := cloneHeaders(options.headers)
	rawBaseline, err := rawRequest(options.method, options.uri, requestTarget, baselineHeaders, "", options.timeout)
	if err == nil {
		setTechniqueBaseline(technique, rawBaseline)
	}
}

// requestMethodOverrideQuery tests _method query parameter override.
// Many web frameworks (Rails, Laravel, etc.) support overriding the HTTP method
// via a _method query parameter when the request is POST.
func requestMethodOverrideQuery(options RequestOptions) {
	overrideMethods := []string{"GET", "PUT", "PATCH", "DELETE", "OPTIONS"}

	w := goccm.New(maxGoroutines)
	p := newProgress("method-override-query", len(overrideMethods))

	for _, overrideMethod := range overrideMethods {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(overrideMethod string) {
			defer w.Done()
			defer p.done()

			// Append _method= query parameter
			separator := "?"
			if strings.Contains(options.uri, "?") {
				separator = "&"
			}
			targetURI := options.uri + separator + "_method=" + overrideMethod

			// Send as POST (frameworks only process _method on POST)
			resp, err := requestWithRetry("POST", targetURI, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}

			if isCalibrationMatch(resp.contentLength) {
				return
			}
			result := resultFromResponse("POST "+targetURI, false, "method-override-query", resp)
			attachHTTPReplay(&result, "POST", targetURI, options.headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "method-override-query")
		}(overrideMethod)
	}
	w.WaitAllDone()
	p.finish()
}

func requestMethodOverrideHeaders(options RequestOptions) {
	overrideMethods := []string{"GET", "PUT", "PATCH", "DELETE", "OPTIONS"}
	overrideHeaders := []string{"X-HTTP-Method-Override", "X-HTTP-Method", "X-Method-Override"}
	total := len(overrideMethods) * len(overrideHeaders)

	w := goccm.New(maxGoroutines)
	p := newProgress("method-override-header", total)

	for _, headerName := range overrideHeaders {
		for _, overrideMethod := range overrideMethods {
			time.Sleep(time.Duration(delay) * time.Millisecond)
			w.Wait()
			go func(headerName, overrideMethod string) {
				defer w.Done()
				defer p.done()

				headers := make([]header, len(options.headers))
				copy(headers, options.headers)
				headers = append(headers, header{headerName, overrideMethod})

				resp, err := requestWithRetry("POST", options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
				if err != nil {
					if errors.Is(err, ErrRateLimited) {
						return
					}
					logVerbose(err)
					return
				}
				if isCalibrationMatch(resp.contentLength) {
					return
				}

				result := resultFromResponse(headerName+": "+overrideMethod, false, "method-override-header", resp)
				attachHTTPReplay(&result, "POST", options.uri, headers, "", options.redirect, options.proxy, options.timeout)
				printResponse(result, "method-override-header")
			}(headerName, overrideMethod)
		}
	}
	w.WaitAllDone()
	p.finish()
}

func requestMethodOverrideBody(options RequestOptions) {
	overrideMethods := []string{"PUT", "PATCH", "DELETE"}
	bodyTemplates := []struct {
		contentType string
		body        string
		label       string
	}{
		{"application/x-www-form-urlencoded", "_method=%s", "form _method"},
		{"application/json", `{"_method":"%s"}`, "json _method"},
	}

	total := len(overrideMethods) * len(bodyTemplates)
	w := goccm.New(maxGoroutines)
	p := newProgress("method-override-body", total)

	for _, tpl := range bodyTemplates {
		for _, overrideMethod := range overrideMethods {
			time.Sleep(time.Duration(delay) * time.Millisecond)
			w.Wait()
			go func(tpl struct {
				contentType string
				body        string
				label       string
			}, overrideMethod string) {
				defer w.Done()
				defer p.done()

				body := fmt.Sprintf(tpl.body, overrideMethod)
				headers := make([]header, len(options.headers))
				copy(headers, options.headers)
				headers = append(headers, header{"Content-Type", tpl.contentType})

				resp, err := requestWithRetryBody("POST", options.uri, headers, body, options.proxy, options.rateLimit, options.timeout, options.redirect)
				if err != nil {
					if errors.Is(err, ErrRateLimited) {
						return
					}
					logVerbose(err)
					return
				}
				if isCalibrationMatch(resp.contentLength) {
					return
				}

				result := resultFromResponse(tpl.label+": "+overrideMethod, false, "method-override-body", resp)
				attachHTTPReplay(&result, "POST", options.uri, headers, body, options.redirect, options.proxy, options.timeout)
				printResponse(result, "method-override-body")
			}(tpl, overrideMethod)
		}
	}
	w.WaitAllDone()
	p.finish()
}

func requestPathNormalization(options RequestOptions) {
	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}

	pathValue := parsedURL.Path
	if pathValue == "" || pathValue == "/" {
		return
	}

	trailingSlash := strings.HasSuffix(pathValue, "/")
	trimmedPath := strings.Trim(pathValue, "/")
	segments := strings.Split(trimmedPath, "/")
	if len(segments) == 0 {
		return
	}

	lastSegment := segments[len(segments)-1]
	basePath := "/"
	if len(segments) > 1 {
		basePath = "/" + strings.Join(segments[:len(segments)-1], "/") + "/"
	}
	query := ""
	if parsedURL.RawQuery != "" {
		query = "?" + parsedURL.RawQuery
	}
	baseURL := parsedURL.Scheme + "://" + parsedURL.Host

	payloads := []string{
		baseURL + basePath + "%2e/" + lastSegment + query,
		baseURL + basePath + ".%2e/" + lastSegment + query,
		baseURL + basePath + "%2e%2e/" + lastSegment + query,
		baseURL + basePath + "..%2f" + lastSegment + query,
		baseURL + basePath + "%2e%2e%2f" + lastSegment + query,
		baseURL + basePath + lastSegment + "/." + query,
		baseURL + basePath + lastSegment + "/%2e" + query,
		baseURL + basePath + ";" + lastSegment + query,
	}
	if trailingSlash {
		payloads = append(payloads, baseURL+basePath+lastSegment+"/..;/"+query)
	}
	payloads = removeDuplicates(payloads)

	w := goccm.New(maxGoroutines)
	p := newProgress("path-normalization", len(payloads))

	for _, payload := range payloads {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(payload string) {
			defer w.Done()
			defer p.done()

			resp, err := requestWithRetry(options.method, payload, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}
			if isCalibrationMatch(resp.contentLength) {
				return
			}

			result := resultFromResponse(payload, false, "path-normalization", resp)
			attachHTTPReplay(&result, options.method, payload, options.headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "path-normalization")
		}(payload)
	}
	w.WaitAllDone()
	p.finish()
}

func requestSuffixTricks(options RequestOptions) {
	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}

	pathValue := parsedURL.Path
	if pathValue == "" || pathValue == "/" {
		return
	}

	baseURL := parsedURL.Scheme + "://" + parsedURL.Host
	query := ""
	if parsedURL.RawQuery != "" {
		query = "?" + parsedURL.RawQuery
	}

	payloads := []string{
		baseURL + pathValue + ".json" + query,
		baseURL + pathValue + ".css" + query,
		baseURL + pathValue + ".js" + query,
		baseURL + pathValue + ";" + query,
		baseURL + pathValue + ";index.html" + query,
		baseURL + pathValue + "?" + "download=1",
		baseURL + pathValue + "?" + "format=json",
		baseURL + pathValue + "?" + "output=1",
	}
	if parsedURL.RawQuery != "" {
		payloads = append(payloads,
			baseURL+pathValue+";?"+parsedURL.RawQuery,
			baseURL+pathValue+".json?"+parsedURL.RawQuery,
		)
	}
	payloads = removeDuplicates(payloads)

	w := goccm.New(maxGoroutines)
	p := newProgress("suffix-tricks", len(payloads))

	for _, payload := range payloads {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(payload string) {
			defer w.Done()
			defer p.done()

			resp, err := requestWithRetry(options.method, payload, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}
			if isCalibrationMatch(resp.contentLength) {
				return
			}

			result := resultFromResponse(payload, false, "suffix-tricks", resp)
			attachHTTPReplay(&result, options.method, payload, options.headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "suffix-tricks")
		}(payload)
	}
	w.WaitAllDone()
	p.finish()
}

func requestHeaderConfusion(options RequestOptions) {
	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}

	pathValue := parsedURL.RequestURI()
	if pathValue == "" {
		pathValue = "/"
	}
	pathOnly := parsedURL.Path
	if pathOnly == "" {
		pathOnly = "/"
	}

	payloads := []struct {
		headers []header
		label   string
	}{
		{[]header{{"X-Original-URL", pathOnly}}, "X-Original-URL -> path"},
		{[]header{{"X-Rewrite-URL", pathOnly}}, "X-Rewrite-URL -> path"},
		{[]header{{"X-Forwarded-Uri", pathOnly}}, "X-Forwarded-Uri -> path"},
		{[]header{{"X-Forwarded-URL", pathValue}}, "X-Forwarded-URL -> request-uri"},
		{[]header{{"X-Forwarded-Prefix", pathOnly}}, "X-Forwarded-Prefix -> path"},
		{[]header{{"X-Original-URL", "/"}, {"X-Rewrite-URL", pathOnly}}, "rewrite root/path split"},
		{[]header{{"Front-End-Https", "on"}, {"X-Forwarded-Proto", "https"}}, "front-end https"},
		{[]header{{"X-Original-Host", "localhost"}, {"X-Forwarded-Host", parsedURL.Host}}, "original/forwarded host"},
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("header-confusion", len(payloads))

	for _, payload := range payloads {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(payload struct {
			headers []header
			label   string
		}) {
			defer w.Done()
			defer p.done()

			headers := make([]header, len(options.headers))
			copy(headers, options.headers)
			headers = append(headers, payload.headers...)

			resp, err := requestWithRetry(options.method, options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}
			if isCalibrationMatch(resp.contentLength) {
				return
			}

			result := resultFromResponse(payload.label, false, "header-confusion", resp)
			attachHTTPReplay(&result, options.method, options.uri, headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "header-confusion")
		}(payload)
	}
	w.WaitAllDone()
	p.finish()
}

func requestHostOverride(options RequestOptions) {
	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}

	originalHost := parsedURL.Host
	hostname := parsedURL.Hostname()
	if hostname == "" {
		return
	}

	payloads := []struct {
		headers []header
		label   string
	}{
		{[]header{{"X-Forwarded-Host", "localhost"}}, "X-Forwarded-Host localhost"},
		{[]header{{"X-Forwarded-Host", hostname + "."}}, "X-Forwarded-Host trailing dot"},
		{[]header{{"X-Forwarded-Server", "localhost"}}, "X-Forwarded-Server localhost"},
		{[]header{{"X-Host", "localhost"}}, "X-Host localhost"},
		{[]header{{"X-HTTP-Host-Override", "localhost"}}, "X-HTTP-Host-Override localhost"},
		{[]header{{"X-Original-Host", "localhost"}, {"X-Forwarded-Host", originalHost}}, "original host split"},
		{[]header{{"Host", hostname + "."}}, "Host trailing dot"},
		{[]header{{"Host", strings.ToUpper(hostname)}}, "Host uppercase"},
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("host-override", len(payloads))

	for _, payload := range payloads {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(payload struct {
			headers []header
			label   string
		}) {
			defer w.Done()
			defer p.done()

			headers := make([]header, len(options.headers))
			copy(headers, options.headers)
			headers = append(headers, payload.headers...)

			resp, err := requestWithRetry(options.method, options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}
			if isCalibrationMatch(resp.contentLength) {
				return
			}

			result := resultFromResponse(payload.label, false, "host-override", resp)
			attachHTTPReplay(&result, options.method, options.uri, headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "host-override")
		}(payload)
	}
	w.WaitAllDone()
	p.finish()
}

func requestForwardedTrust(options RequestOptions) {
	bypass := options.bypassIP
	if bypass == "" {
		bypass = "127.0.0.1"
	}

	payloads := []struct {
		headers []header
		label   string
	}{
		{[]header{{"Forwarded", "for=127.0.0.1;proto=https;host=localhost"}}, "Forwarded localhost"},
		{[]header{{"Forwarded", "for=\"[::1]\";proto=https"}}, "Forwarded IPv6 loopback"},
		{[]header{{"Forwarded", "for=127.0.0.1, for=198.51.100.1"}}, "Forwarded chain localhost first"},
		{[]header{{"Forwarded", "for=198.51.100.1, for=127.0.0.1"}}, "Forwarded chain localhost last"},
		{[]header{{"Client-IP", bypass}, {"Cluster-Client-IP", bypass}}, "Client-IP cluster pair"},
		{[]header{{"X-Forwarded-For", bypass}, {"X-Client-IP", bypass}, {"True-Client-IP", bypass}}, "forwarded client ip trio"},
		{[]header{{"X-Original-Remote-Addr", bypass}, {"X-Remote-IP", bypass}}, "original/remote addr"},
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("forwarded-trust", len(payloads))

	for _, payload := range payloads {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(payload struct {
			headers []header
			label   string
		}) {
			defer w.Done()
			defer p.done()

			headers := make([]header, len(options.headers))
			copy(headers, options.headers)
			headers = append(headers, payload.headers...)

			resp, err := requestWithRetry(options.method, options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}
			if isCalibrationMatch(resp.contentLength) {
				return
			}

			result := resultFromResponse(payload.label, false, "forwarded-trust", resp)
			attachHTTPReplay(&result, options.method, options.uri, headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "forwarded-trust")
		}(payload)
	}
	w.WaitAllDone()
	p.finish()
}

func requestProtoConfusion(options RequestOptions) {
	payloads := []struct {
		headers []header
		label   string
	}{
		{[]header{{"X-Forwarded-Proto", "https"}, {"X-Forwarded-Port", "443"}}, "https 443"},
		{[]header{{"X-Forwarded-Proto", "http"}, {"X-Forwarded-Port", "80"}}, "http 80"},
		{[]header{{"X-Forwarded-Proto", "https"}, {"X-Forwarded-Ssl", "on"}}, "forwarded ssl on"},
		{[]header{{"Front-End-Https", "on"}, {"X-Url-Scheme", "https"}}, "front-end https url-scheme"},
		{[]header{{"X-Forwarded-Protocol", "https"}, {"X-Original-Scheme", "https"}}, "protocol/original scheme"},
		{[]header{{"X-Forwarded-Proto", "https"}, {"X-Forwarded-Host", "localhost"}, {"X-Forwarded-Port", "443"}}, "https host localhost"},
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("proto-confusion", len(payloads))

	for _, payload := range payloads {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(payload struct {
			headers []header
			label   string
		}) {
			defer w.Done()
			defer p.done()

			headers := make([]header, len(options.headers))
			copy(headers, options.headers)
			headers = append(headers, payload.headers...)

			resp, err := requestWithRetry(options.method, options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}
			if isCalibrationMatch(resp.contentLength) {
				return
			}

			result := resultFromResponse(payload.label, false, "proto-confusion", resp)
			attachHTTPReplay(&result, options.method, options.uri, headers, "", options.redirect, options.proxy, options.timeout)
			printResponse(result, "proto-confusion")
		}(payload)
	}
	w.WaitAllDone()
	p.finish()
}

func requestIPEncodingHeaders(options RequestOptions) {
	encodedIPs := []string{
		"127.0.0.1",
		"127.1",
		"0177.0.0.1",
		"0x7f.0x0.0x0.0x1",
		"2130706433",
		"localhost",
		"[::1]",
		"::ffff:127.0.0.1",
	}
	headersToTry := []string{
		"X-Forwarded-For",
		"Client-IP",
		"True-Client-IP",
		"X-Real-IP",
		"CF-Connecting-IP",
	}

	type ipEncodingPayload struct {
		headers []header
		label   string
	}
	var payloads []ipEncodingPayload
	for _, h := range headersToTry {
		for _, ip := range encodedIPs {
			payloads = append(payloads, ipEncodingPayload{
				headers: []header{{h, ip}},
				label:   h + ": " + ip,
			})
		}
	}

	w := goccm.New(maxGoroutines)
	p := newProgress("ip-encoding", len(payloads))

	for _, payload := range payloads {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(payload ipEncodingPayload) {
			defer w.Done()
			defer p.done()

			headers := make([]header, len(options.headers))
			copy(headers, options.headers)
			headers = append(headers, payload.headers...)

			resp, err := requestWithRetry(options.method, options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				if errors.Is(err, ErrRateLimited) {
					return
				}
				logVerbose(err)
				return
			}
			if isCalibrationMatch(resp.contentLength) {
				return
			}

			result := resultFromResponse(payload.label, false, "ip-encoding", resp)
			result.reproCurl = buildCurlCommand(options.method, options.uri, headers, "", options.redirect, options.proxy)
			printResponse(result, "ip-encoding")
		}(payload)
	}
	w.WaitAllDone()
	p.finish()
}

func requestRawDuplicates(options RequestOptions) {
	if options.proxy != nil && options.proxy.String() != "" {
		log.Printf("[!] Skipping raw duplicates: raw HTTP mode currently does not support proxies")
		return
	}

	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}
	requestTarget := parsedURL.RequestURI()
	if requestTarget == "" {
		requestTarget = "/"
	}

	bypass := options.bypassIP
	if bypass == "" {
		bypass = "127.0.0.1"
	}

	payloads := []struct {
		headers []header
		label   string
	}{
		{[]header{{"X-Forwarded-For", bypass}, {"X-Forwarded-For", "1.1.1.1"}}, "duplicate x-forwarded-for"},
		{[]header{{"X-Original-URL", "/"}, {"X-Original-URL", parsedURL.Path}}, "duplicate x-original-url"},
		{[]header{{"X-Rewrite-URL", "/"}, {"X-Rewrite-URL", parsedURL.Path}}, "duplicate x-rewrite-url"},
		{[]header{{"Forwarded", "for=127.0.0.1;host=localhost"}, {"Forwarded", "for=198.51.100.1;host=" + parsedURL.Host}}, "duplicate forwarded chain"},
	}

	baselineHeaders := make([]header, len(options.headers))
	copy(baselineHeaders, options.headers)
	setRawTechniqueBaseline("raw-duplicates", options, requestTarget)

	w := goccm.New(maxGoroutines)
	p := newProgress("raw-duplicates", len(payloads))

	for _, payload := range payloads {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(payload struct {
			headers []header
			label   string
		}) {
			defer w.Done()
			defer p.done()

			headers := make([]header, len(options.headers))
			copy(headers, options.headers)
			headers = append(headers, payload.headers...)

			resp, err := rawRequest(options.method, options.uri, requestTarget, headers, "", options.timeout)
			if err != nil {
				logVerbose(err)
				return
			}
			if isCalibrationMatch(resp.contentLength) {
				return
			}

			result := resultFromResponse(payload.label, false, "raw-duplicates", resp)
			attachRawReplay(&result, options.method, options.uri, requestTarget, headers, "", options.timeout)
			printResponse(result, "raw-duplicates")
		}(payload)
	}
	w.WaitAllDone()
	p.finish()
}

func requestRawAuthority(options RequestOptions) {
	if options.proxy != nil && options.proxy.String() != "" {
		log.Printf("[!] Skipping raw authority: raw HTTP mode currently does not support proxies")
		return
	}

	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}
	requestTarget := parsedURL.RequestURI()
	if requestTarget == "" {
		requestTarget = "/"
	}

	payloads := []struct {
		headers []header
		label   string
	}{
		{[]header{{"Host", parsedURL.Host}, {"Host", "localhost"}}, "duplicate host localhost"},
		{[]header{{"Host", "localhost"}, {"Host", parsedURL.Host}}, "duplicate host original last"},
		{[]header{{"Forwarded", "host=localhost;proto=https"}, {"Forwarded", "host=" + parsedURL.Host + ";proto=https"}}, "duplicate forwarded host"},
		{[]header{{"Host", parsedURL.Host}, {"X-HTTP-Host-Override", "localhost"}}, "host plus override localhost"},
	}

	setRawTechniqueBaseline("raw-authority", options, requestTarget)

	w := goccm.New(maxGoroutines)
	p := newProgress("raw-authority", len(payloads))

	for _, payload := range payloads {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(payload struct {
			headers []header
			label   string
		}) {
			defer w.Done()
			defer p.done()

			headers := make([]header, len(options.headers))
			copy(headers, options.headers)
			headers = append(headers, payload.headers...)

			resp, err := rawRequest(options.method, options.uri, requestTarget, headers, "", options.timeout)
			if err != nil {
				logVerbose(err)
				return
			}
			if isCalibrationMatch(resp.contentLength) {
				return
			}

			result := resultFromResponse(payload.label, false, "raw-authority", resp)
			attachRawReplay(&result, options.method, options.uri, requestTarget, headers, "", options.timeout)
			printResponse(result, "raw-authority")
		}(payload)
	}
	w.WaitAllDone()
	p.finish()
}

func requestRawDesync(options RequestOptions) {
	if options.proxy != nil && options.proxy.String() != "" {
		log.Printf("[!] Skipping raw desync: raw HTTP mode currently does not support proxies")
		return
	}

	parsedURL, err := url.Parse(options.uri)
	if err != nil {
		log.Println(err)
		return
	}
	requestTarget := parsedURL.RequestURI()
	if requestTarget == "" {
		requestTarget = "/"
	}

	payloads := []struct {
		method        string
		requestTarget string
		headers       []header
		body          string
		label         string
	}{
		{
			method:        "POST",
			requestTarget: requestTarget,
			headers: []header{
				{"Transfer-Encoding", "chunked"},
				{"Content-Length", "4"},
				{"Content-Type", "application/x-www-form-urlencoded"},
			},
			body:  "0\r\n\r\n",
			label: "CL+TE chunked zero-body",
		},
		{
			method:        "POST",
			requestTarget: requestTarget,
			headers: []header{
				{"Content-Length", "0"},
				{"Content-Length", "8"},
				{"Content-Type", "application/x-www-form-urlencoded"},
			},
			body:  "_=1&_2=2",
			label: "duplicate content-length",
		},
		{
			method:        "OPTIONS",
			requestTarget: "*",
			headers: []header{
				{"Content-Length", "0"},
			},
			body:  "",
			label: "OPTIONS *",
		},
		{
			method:        "GET",
			requestTarget: requestTarget,
			headers: []header{
				{"Transfer-Encoding", "chunked"},
				{"Content-Length", "0"},
			},
			body:  "0\r\n\r\n",
			label: "GET with chunked framing",
		},
		{
			method:        "POST",
			requestTarget: requestTarget,
			headers: []header{
				{"Transfer-Encoding", "chunked"},
				{"Transfer-Encoding", "identity"},
				{"Content-Length", "4"},
			},
			body:  "0\r\n\r\n",
			label: "duplicate transfer-encoding",
		},
		{
			method:        "POST",
			requestTarget: requestTarget,
			headers: []header{
				{"Transfer-Encoding", " chunked"},
				{"Content-Length", "4"},
			},
			body:  "0\r\n\r\n",
			label: "spaced transfer-encoding",
		},
		{
			method:        "GET",
			requestTarget: requestTarget + " HTTP/1.1",
			headers: []header{
				{"Connection", "close"},
			},
			body:  "",
			label: "request-line confusion",
		},
	}

	setRawTechniqueBaseline("raw-desync", options, requestTarget)

	w := goccm.New(maxGoroutines)
	p := newProgress("raw-desync", len(payloads))
	for _, payload := range payloads {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(payload struct {
			method        string
			requestTarget string
			headers       []header
			body          string
			label         string
		}) {
			defer w.Done()
			defer p.done()

			headers := make([]header, len(options.headers))
			copy(headers, options.headers)
			headers = append(headers, payload.headers...)

			resp, err := rawRequest(payload.method, options.uri, payload.requestTarget, headers, payload.body, options.timeout)
			if err != nil {
				logVerbose(err)
				return
			}
			if isCalibrationMatch(resp.contentLength) {
				return
			}

			result := resultFromResponse(payload.label, false, "raw-desync", resp)
			attachRawReplay(&result, payload.method, options.uri, payload.requestTarget, headers, payload.body, options.timeout)
			printResponse(result, "raw-desync")
		}(payload)
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
		uri:             uri,
		headers:         headers,
		method:          method,
		proxy:           userProxy,
		userAgent:       userAgent,
		redirect:        redirect,
		folder:          folder,
		bypassIP:        bypassIP,
		timeout:         timeout,
		rateLimit:       rateLimit,
		verbose:         verbose,
		techniques:      techniques,
		reqHeaders:      reqHeaders,
		banner:          banner,
		autocalibrate:   !verbose && !noCalibrate,
		strictCalibrate: strictCalibrate,
		rawHTTP:         rawHTTP,
	}
	opts.frontendHints = inferFrontendHints(uri, ResponseInfo{})
	if !techniqueExplicit {
		opts.techniques = prioritizeTechniques(opts.techniques, opts.frontendHints)
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

	techniqueBaselinesMutex.Lock()
	for k := range techniqueBaselines {
		delete(techniqueBaselines, k)
	}
	storedGlobalBaseline = ResponseInfo{}
	techniqueBaselinesMutex.Unlock()

	topFindingsMutex.Lock()
	topFindings = nil
	topFindingsMutex.Unlock()
	printedBaselineHeader = false
	printedFindingsHeader = false

	techniqueHeadersMutex.Lock()
	for k := range printedTechniqueHeaders {
		delete(printedTechniqueHeaders, k)
	}
	for k := range executedTechniques {
		delete(executedTechniques, k)
	}
	for k := range producedTechniques {
		delete(producedTechniques, k)
	}
	for k := range seenTechniqueFamilies {
		delete(seenTechniqueFamilies, k)
	}
	for k := range suppressedTechniqueFamilies {
		delete(suppressedTechniqueFamilies, k)
	}
	for k := range seenGlobalFamilies {
		delete(seenGlobalFamilies, k)
	}
	for k := range suppressedCrossTechniqueFamilies {
		delete(suppressedCrossTechniqueFamilies, k)
	}
	techniqueHeadersMutex.Unlock()

	for k := range verbTamperingResults {
		delete(verbTamperingResults, k)
	}
}

// executeTechniques runs the selected bypass techniques based on the provided options.
func executeTechniques(options RequestOptions) {
	for _, tech := range options.techniques {
		markTechniqueExecuted(tech)
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
		case "http-parser":
			requestHTTPParser(options)
		case "path-case":
			requestPathCaseSwitching(options)
			printSuppressedSummary("path-case-switching")
		case "unicode":
			requestUnicodeEncoding(options)
			printSuppressedSummary("unicode-encoding")
		case "payload-position":
			requestPayloadPositions(options)
			printSuppressedSummary("payload-position")
		case "hop-by-hop":
			requestHopByHop(options)
			printSuppressedSummary("hop-by-hop")
		case "absolute-uri":
			requestAbsoluteURI(options)
			printSuppressedSummary("absolute-uri")
		case "method-override":
			requestMethodOverrideQuery(options)
			requestMethodOverrideHeaders(options)
			requestMethodOverrideBody(options)
			printSuppressedSummary("method-override-query")
			printSuppressedSummary("method-override-header")
			printSuppressedSummary("method-override-body")
		case "path-normalization":
			requestPathNormalization(options)
			printSuppressedSummary("path-normalization")
		case "suffix-tricks":
			requestSuffixTricks(options)
			printSuppressedSummary("suffix-tricks")
		case "header-confusion":
			requestHeaderConfusion(options)
			printSuppressedSummary("header-confusion")
		case "host-override":
			requestHostOverride(options)
			printSuppressedSummary("host-override")
		case "forwarded-trust":
			requestForwardedTrust(options)
			printSuppressedSummary("forwarded-trust")
		case "proto-confusion":
			requestProtoConfusion(options)
			printSuppressedSummary("proto-confusion")
		case "ip-encoding":
			requestIPEncodingHeaders(options)
			printSuppressedSummary("ip-encoding")
		case "raw-duplicates":
			requestRawDuplicates(options)
			printSuppressedSummary("raw-duplicates")
		case "raw-authority":
			requestRawAuthority(options)
			printSuppressedSummary("raw-authority")
		case "raw-desync":
			requestRawDesync(options)
			printSuppressedSummary("raw-desync")
		default:
			fmt.Printf("Unrecognized technique. %s\n", tech)
			fmt.Print("Available techniques: verbs, verbs-case, headers, endpaths, midpaths, double-encoding, unicode, http-versions, http-parser, path-case, hop-by-hop, absolute-uri, method-override, path-normalization, suffix-tricks, header-confusion, host-override, forwarded-trust, proto-confusion, ip-encoding, raw-duplicates, raw-authority, raw-desync\n")
		}
	}
}

// requester is the main function that runs all the tests.
func requester(uri string, proxy string, userAgent string, reqHeaders []string, bypassIP string, folder string, method string, verbose bool, techniques []string, banner bool, rateLimit bool, timeout int, redirect bool, randomAgent bool) {
	setVerbose(verbose)
	setDefaultSc(0)
	setDefaultRespCl(0)
	setFragmentCl(0)

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
	markTechniqueExecuted("default")
	executeTechniques(options)
	printSilentTechniqueSummary()
	printTopFindings(topLimit)
}
