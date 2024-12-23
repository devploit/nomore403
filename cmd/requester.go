package cmd

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/fatih/color"
	"github.com/slicingmelon/go-rawurlparser"
	"github.com/zenthangplus/goccm"
)

type Result struct {
	line          string
	statusCode    int
	contentLength int
	defaultReq    bool
}

type RequestOptions struct {
	uri        string
	headers    []header
	method     string
	proxy      *url.URL
	userAgent  string
	redirect   bool
	folder     string
	bypassIP   string
	timeout    int
	rateLimit  bool
	techniques []string
	verbose    bool
	reqHeaders []string
	banner     bool
}

var _verbose bool
var defaultSc int
var defaultCl int
var printMutex = &sync.Mutex{}
var uniqueResults = make(map[string]bool)

// printResponse prints the results of HTTP requests in a tabular format with colored output based on the status codes.
func printResponse(result Result) {
	printMutex.Lock()
	defer printMutex.Unlock()

	// Check if status code filtering is enabled
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

	// Check for unique output if enabled
	if uniqueOutput {
		key := fmt.Sprintf("%d-%d", result.statusCode, result.contentLength)
		if uniqueResults[key] {
			return
		}
		uniqueResults[key] = true
	}

	resultContentLength := strconv.Itoa(result.contentLength) + " bytes"

	var code string
	switch result.statusCode {
	case 200, 201, 202, 203, 204, 205, 206:
		code = color.GreenString(strconv.Itoa(result.statusCode))
	case 300, 301, 302, 303, 304, 307, 308:
		code = color.YellowString(strconv.Itoa(result.statusCode))
	case 400, 401, 402, 403, 404, 405, 406, 407, 408, 413, 429:
		code = color.RedString(strconv.Itoa(result.statusCode))
	case 500, 501, 502, 503, 504, 505, 511:
		code = color.MagentaString(strconv.Itoa(result.statusCode))
	}

	if !_verbose {
		if ((defaultSc == result.statusCode) && (defaultCl == result.contentLength) || result.contentLength == 0 || result.statusCode == 404 || result.statusCode == 400) && !result.defaultReq {
			return
		} else {
			fmt.Printf("%s \t%20s %s\n", code, color.BlueString(resultContentLength), result.line)
		}
	} else {
		fmt.Printf("%s \t%20s %s\n", code, color.BlueString(resultContentLength), result.line)
	}
}

func showInfo(options RequestOptions) {
	var statusCodeStrings []string

	for _, code := range statusCodes {
		statusCodeStrings = append(statusCodeStrings, code)
	}
	statusCodesString := strings.Join(statusCodeStrings, ", ")

	if !nobanner {
		fmt.Println(`
    ________  ________  ________  ________  ________  ________  ________  ________  ________
   ╱     ╱  ╲╱        ╲╱    ╱   ╲╱        ╲╱        ╲╱        ╲╱    ╱   ╲╱        ╲╱__      ╲
  ╱         ╱    ╱    ╱         ╱    ╱    ╱    ╱    ╱       __╱         ╱    ╱    ╱__       ╱
 ╱         ╱         ╱         ╱         ╱        _╱       __/____     ╱         ╱         ╱
 ╲__╱_____╱╲________╱╲__╱__╱__╱╲________╱╲____╱___╱╲________╱    ╱____╱╲________╱╲________╱                                   
	`)
		fmt.Printf("%s \t\t%s\n", "Target:", options.uri)
		if len(options.reqHeaders[0]) != 0 {
			for _, header := range options.headers {
				fmt.Printf("%s \t\t%s\n", "Headers:", header)
			}
		} else {
			fmt.Printf("%s \t\t%s\n", "Headers:", "false")
		}
		if len(options.proxy.Host) != 0 {
			fmt.Printf("%s \t\t\t%s\n", "Proxy:", options.proxy.Host)
		} else {
			fmt.Printf("%s \t\t\t%s\n", "Proxy:", "false")
		}
		fmt.Printf("%s \t\t%s\n", "User Agent:", options.userAgent)
		fmt.Printf("%s \t\t%s\n", "Method:", options.method)
		fmt.Printf("%s \t%s\n", "Payloads folder:", options.folder)
		if len(bypassIP) != 0 {
			fmt.Printf("%s \t%s\n", "Custom bypass IP:", options.bypassIP)
		} else {
			fmt.Printf("%s \t%s\n", "Custom bypass IP:", "false")
		}
		fmt.Printf("%s \t%s\n", "Follow Redirects:", strconv.FormatBool(options.redirect))
		fmt.Printf("%s \t%s\n", "Rate Limit detection:", strconv.FormatBool(options.rateLimit))
		fmt.Printf("%s \t\t%s\n", "Status:", statusCodesString)
		fmt.Printf("%s \t\t%d\n", "Timeout (ms):", options.timeout)
		fmt.Printf("%s \t\t%d\n", "Delay (ms):", delay)
		fmt.Printf("%s \t\t%s\n", "Techniques:", strings.Join(options.techniques, ", "))
		fmt.Printf("%s \t\t%t\n", "Unique:", uniqueOutput)
		fmt.Printf("%s \t\t%t\n", "Verbose:", options.verbose)
	}
}

// generateCaseCombinations generates all combinations of uppercase and lowercase letters for a given string.
func generateCaseCombinations(s string) []string {
	if len(s) == 0 {
		return []string{""}
	}

	firstCharCombinations := []string{string(unicode.ToLower(rune(s[0]))), string(unicode.ToUpper(rune(s[0])))}
	subCombinations := generateCaseCombinations(s[1:])
	combinations := []string{}

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
	rand.Seed(time.Now().UnixNano())
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

	var results []Result

	statusCode, response, err := request(options.method, options.uri, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
	if err != nil {
		log.Println(err)
	}

	results = append(results, Result{options.method, statusCode, len(response), true})
	printResponse(Result{uri, statusCode, len(response), true})
	for _, result := range results {
		defaultSc = result.statusCode
		defaultCl = result.contentLength
	}
}

// requestMethods makes HTTP requests using a list of methods from a file and prints the results.
func requestMethods(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━━━━ VERB TAMPERING ━━━━━━━━━━━━━━━")

	var lines []string
	lines, err := parseFile(options.folder + "/httpmethods")
	if err != nil {
		log.Fatalf("Error reading /httpmethods file: %v", err)
	}

	w := goccm.New(maxGoroutines)

	for _, line := range lines {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			statusCode, response, err := request(line, options.uri, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				log.Println(err)
			}

			printResponse(Result{line, statusCode, len(response), false})
			w.Done()
		}(line)
	}
	w.WaitAllDone()
}

// requestMethodsCaseSwitching makes HTTP requests using a list of methods from a file and prints the results.
func requestMethodsCaseSwitching(options RequestOptions) {
	color.Cyan("\n━━━━━━━ VERB TAMPERING CASE SWITCHING ━━━━━━━━")

	var lines []string
	lines, err := parseFile(options.folder + "/httpmethods")
	if err != nil {
		log.Fatalf("Error reading /httpmethods file: %v", err)
	}

	w := goccm.New(maxGoroutines)

	for _, line := range lines {
		methodCombinations := generateCaseCombinations(line)
		filteredCombinations := filterOriginalMethod(line, methodCombinations)
		selectedCombinations := selectRandomCombinations(filteredCombinations, 50)
		for _, method := range selectedCombinations {
			time.Sleep(time.Duration(delay) * time.Millisecond)
			w.Wait()
			go func(method string) {
				statusCode, response, err := request(method, options.uri, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
				if err != nil {
					log.Println(err)
				}
				printResponse(Result{method, statusCode, len(response), false})
				w.Done()
			}(method)
		}
	}
	w.WaitAllDone()
}

// requestHeaders makes HTTP requests using a list of headers from a file and prints the results. It can also bypass IP address restrictions by specifying a bypass IP address.
func requestHeaders(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━━━━━━━ HEADERS ━━━━━━━━━━━━━━━━━━━")

	var lines []string
	lines, err := parseFile(options.folder + "/headers")
	if err != nil {
		log.Fatalf("Error reading /headers file: %v", err)
	}

	var ips []string
	if len(options.bypassIP) != 0 {
		ips = []string{options.bypassIP}
	} else {
		ips, err = parseFile(options.folder + "/ips")
		if err != nil {
			log.Fatalf("Error reading /ips file: %v", err)
		}
	}

	simpleheaders, err := parseFile(options.folder + "/simpleheaders")
	if err != nil {
		log.Fatalf("Error reading /simpleheaders file: %v", err)
	}

	w := goccm.New(maxGoroutines)

	for _, ip := range ips {
		for _, line := range lines {
			time.Sleep(time.Duration(delay) * time.Millisecond)
			w.Wait()
			go func(line, ip string) {
				headers := append(options.headers, header{line, ip})

				statusCode, response, err := request(options.method, options.uri, headers, options.proxy, options.rateLimit, options.timeout, options.redirect)

				if err != nil {
					log.Println(err)
				}

				printResponse(Result{line + ": " + ip, statusCode, len(response), false})
				w.Done()
			}(line, ip)
		}
	}

	for _, simpleheader := range simpleheaders {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			x := strings.Split(line, " ")
			headers := append(options.headers, header{x[0], x[1]})

			statusCode, response, err := request(options.method, options.uri, headers, options.proxy, rateLimit, options.timeout, redirect)
			if err != nil {
				log.Println(err)
			}

			printResponse(Result{line, statusCode, len(response), false})
			w.Done()
		}(simpleheader)
	}
	w.WaitAllDone()
}

// requestEndPaths makes HTTP requests using a list of custom end paths from a file and prints the results.
func requestEndPaths(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━━━━ CUSTOM PATHS ━━━━━━━━━━━━━━━━━")

	var lines []string
	lines, err := parseFile(options.folder + "/endpaths")
	if err != nil {
		log.Fatalf("Error reading custom paths file: %v", err)
	}

	w := goccm.New(maxGoroutines)

	for _, line := range lines {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			statusCode, response, err := request(options.method, options.uri+line, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				log.Println(err)
			}

			printResponse(Result{uri + line, statusCode, len(response), false})
			w.Done()
		}(line)
	}

	w.WaitAllDone()
}

// requestMidPaths makes HTTP requests using a list of custom mid-paths from a file and prints the results.
func requestMidPaths(options RequestOptions) {
	var lines []string
	lines, err := parseFile(options.folder + "/midpaths")
	if err != nil {
		log.Fatalf("Error reading custom paths file: %v", err)
	}
	// fmt.Println(options.uri, "ádkohasdjkhasdjklohans")
	x := strings.Split(options.uri, "/")
	var uripath string

	parsedURL, err := rawurlparser.RawURLParse(options.uri)
	if err != nil {
		log.Println(err)
	}
	if parsedURL.Path != "" && parsedURL.Path != "/" {
		if options.uri[len(options.uri)-1:] == "/" {
			uripath = x[len(x)-2]
		} else {
			uripath = x[len(x)-1]
		}

		baseuri := strings.ReplaceAll(options.uri, uripath, "")
		baseuri = baseuri[:len(baseuri)-1]

		w := goccm.New(maxGoroutines)

		for _, line := range lines {
			time.Sleep(time.Duration(delay) * time.Millisecond)
			w.Wait()
			go func(line string) {
				var fullpath string
				if options.uri[len(options.uri)-1:] == "/" {
					fullpath = baseuri + line + uripath + "/"
				} else {
					fullpath = baseuri + "/" + line + uripath
				}

				statusCode, response, err := request(options.method, fullpath, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
				if err != nil {
					log.Println(err)
				}

				printResponse(Result{fullpath, statusCode, len(response), false})
				w.Done()
			}(line)
		}
		w.WaitAllDone()
	}
}

// requestHttpVersions makes HTTP requests using a list of HTTP versions from a file and prints the results. If server responds with an unique version it is because is not accepting the version provided.
func requestHttpVersions(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━━━━ HTTP VERSIONS ━━━━━━━━━━━━━━━━")

	httpVersions := []string{"--http1.0", "--http1.1", "--http2"}

	for _, version := range httpVersions {
		headerStrings := make([]string, len(options.headers))
		for i, h := range options.headers {
			headerStrings[i] = h.key + ": " + h.value
		}
		res := curlRequest(options.uri, headerStrings, options.proxy.Host, version)
		printResponse(res)
	}

}

func curlRequest(url string, headers []string, proxy string, httpVersion string) Result {
	args := []string{"-i", "-s", httpVersion}
	for _, header := range headers {
		args = append(args, "-H", header)
	}
	if proxy != "" {
		args = append(args, "-x", proxy)
	}
	if redirect {
		args = append(args, "-L")
	}
	args = append(args, "--insecure")
	args = append(args, url)
	cmd := exec.Command("curl", args...)
	// fmt.Println(cmd.String())
	out, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}

	return parseCurlOutput(string(out), httpVersion)
}

func parseCurlOutput(output string, httpVersion string) Result {
	httpVersionOutput := strings.ReplaceAll(httpVersion, "--http", "HTTP/")

	// Split by two line breaks to separate proxy and server responses
	responses := strings.Split(output, "\r\n\r\n")

	// If there is more than one answer, take the last one, which is the one from the server
	serverResponse := responses[len(responses)-2]

	lines := strings.SplitN(serverResponse, "\n", 2)
	parts := strings.SplitN(lines[0], " ", 3)

	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Println(err)
		return Result{}
	}
	return Result{httpVersionOutput, statusCode, len(output), false}
}

// requestPathCaseSwitching makes HTTP requests by capitalizing each letter in the last part of the URI and try to use URL encoded characters.
func requestPathCaseSwitching(options RequestOptions) {
	color.Cyan("\n━━━━━━━━━━━━ PATH CASE SWITCHING ━━━━━━━━━━━━━")

	parsedURL, err := rawurlparser.RawURLParse(options.uri)
	if err != nil {
	 	log.Println(err)
	 	return
	}

	baseuri := parsedURL.Scheme + "://" + parsedURL.Host
	uripath := strings.Trim(parsedURL.Path, "/")

	if len(uripath) == 0 {
		log.Println("No path to modify")
		return
	}

	pathCombinations := generateCaseCombinations(uripath)
	selectedPaths := selectRandomCombinations(pathCombinations, 60)
	w := goccm.New(maxGoroutines)

	for _, path := range selectedPaths {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(path string) {
			var fullpath string
			if strings.HasSuffix(options.uri, "/") {
				fullpath = baseuri + "/" + path + "/"
			} else {
				fullpath = baseuri + "/" + path
			}

			statusCode, response, err := request(options.method, fullpath, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				log.Println(err)
			}

			printResponse(Result{fullpath, statusCode, len(response), false})
			w.Done()
		}(path)
	}

	for _, z := range uripath {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(z rune) {
			encodedChar := fmt.Sprintf("%%%X", z) // convert rune to its hexadecimal ASCII value
			newpath := strings.Replace(uripath, string(z), encodedChar, 1)

			var fullpath string
			if options.uri[len(options.uri)-1:] == "/" {
				fullpath = baseuri + "/" + newpath + "/"
			} else {
				fullpath = baseuri + "/" + newpath
			}

			statusCode, response, err := request(options.method, fullpath, options.headers, options.proxy, options.rateLimit, options.timeout, options.redirect)
			if err != nil {
				log.Println(err)
			}

			printResponse(Result{fullpath, statusCode, len(response), false})
			w.Done()
		}(z)
	}

	w.WaitAllDone()
}

// randomLine take a random line from a file
func randomLine(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	// Seed the random number generator
	rand.Seed(time.Now().UnixNano())
	// Select a random Line
	randomLine := lines[rand.Intn(len(lines))]

	return randomLine, nil
}

// requester is the main function that runs all the tests.
func requester(uri string, proxy string, userAgent string, reqHeaders []string, bypassIP string, folder string, method string, verbose bool, techniques []string, banner bool, rateLimit bool, timeout int, redirect bool, randomAgent bool) {

	// Set up proxy if provided.
	if len(proxy) != 0 {
		if !strings.Contains(proxy, "http") {
			proxy = "http://" + proxy
		}
		color.Magenta("\n[*] USING PROXY: %s\n", proxy)
	}
	userProxy, _ := url.Parse(proxy)

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
			fmt.Println("Error reading the file:", err)
			return
		}
		userAgent = line
	}

	// Set default request method to GET.
	if len(method) == 0 {
		method = "GET"
	}

	headers := []header{
		{"User-Agent", userAgent},
	}

	// Parse custom headers from CLI arguments and add them to the headers slice.
	if len(reqHeaders[0]) != 0 {
		for _, _header := range reqHeaders {
			headerSplit := strings.Split(_header, ":")
			headers = append(headers, header{headerSplit[0], strings.Join(headerSplit[1:], "")})
		}
	}

	_verbose = verbose

	options := RequestOptions{
		uri:        uri,
		headers:    headers,
		method:     method,
		proxy:      userProxy,
		userAgent:  userAgent,
		redirect:   redirect,
		folder:     folder,
		bypassIP:   bypassIP,
		timeout:    timeout,
		rateLimit:  rateLimit,
		verbose:    verbose,
		techniques: techniques,
		reqHeaders: reqHeaders,
		banner:     banner,
	}

	// Reset uniqueResults map before starting new requests
	uniqueResults = make(map[string]bool)

	// Call each function that will send HTTP requests with different variations of headers and URLs.
	showInfo(options)

	for _, tech := range techniques {
		switch tech {
		case "verbs":
			requestMethods(options)
		case "verbs-case":
			requestMethodsCaseSwitching(options)
		case "headers":
			requestHeaders(options)
		case "endpaths":
			requestEndPaths(options)
		case "midpaths":
			requestMidPaths(options)
		case "http-versions":
			requestHttpVersions(options)
		case "path-case":
			requestPathCaseSwitching(options)
		default:
			fmt.Printf("Unrecognized technique. %s\n", tech)
			fmt.Print("Available techniques: verbs, verbs-case, headers, endpaths, midpaths, http-versions, path-case\n")
		}
	}
}
