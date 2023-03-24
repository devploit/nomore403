package cmd

import (
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/cheynewallace/tabby"
	"github.com/fatih/color"
	"github.com/zenthangplus/goccm"
)

type Result struct {
	line          string
	statusCode    int
	contentLength int
}

var _verbose bool
var default_sc int
var default_cl int

// printResponse prints the results of HTTP requests in a tabular format with colored output based on the status codes.
func printResponse(results []Result) {
	t := tabby.New()

	var code string
	for _, result := range results {
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
		if _verbose != true {
			if default_sc == result.statusCode {
				continue
			} else {
				t.AddLine(code, color.BlueString(strconv.Itoa(result.contentLength)+" bytes"), result.line)
			}
		} else {
			t.AddLine(code, color.BlueString(strconv.Itoa(result.contentLength)+" bytes"), result.line)
		}

	}
	t.Print()

}

// requestDefault makes HTTP request to check the default response
func requestDefault(uri string, headers []header, proxy *url.URL, method string) {
	color.Cyan("\n[####] DEFAULT REQUEST [####]")

	results := []Result{}

	statusCode, response, err := request(method, uri, headers, proxy)
	if err != nil {
		log.Println(err)
	}

	results = append(results, Result{method, statusCode, len(response)})
	printResponse(results)
	for _, result := range results {
		default_sc = result.statusCode
		default_cl = result.contentLength
	}
}

// requestMethods makes HTTP requests using a list of methods from a file and prints the results.
func requestMethods(uri string, headers []header, proxy *url.URL, folder string) {
	color.Cyan("\n[####] VERB TAMPERING [####]")

	var lines []string
	lines, err := parseFile(folder + "/httpmethods")
	if err != nil {
		log.Fatalf("Error reading /httpmethods file: %v", err)
	}

	w := goccm.New(max_goroutines)

	results := []Result{}

	for _, line := range lines {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			statusCode, response, err := request(line, uri, headers, proxy)
			if err != nil {
				log.Println(err)
			}

			results = append(results, Result{line, statusCode, len(response)})
			w.Done()
		}(line)
	}
	w.WaitAllDone()
	printResponse(results)
}

// requestHeaders makes HTTP requests using a list of headers from a file and prints the results. It can also bypass IP address restrictions by specifying a bypass IP address.
func requestHeaders(uri string, headers []header, proxy *url.URL, bypassIp string, folder string, method string) {
	color.Cyan("\n[####] HEADERS [####]")

	var lines []string
	lines, err := parseFile(folder + "/headers")
	if err != nil {
		log.Fatalf("Error reading /headers file: %v", err)
	}

	var ips []string
	if len(bypassIp) != 0 {
		ips = []string{bypassIp}
	} else {
		ips, err = parseFile(folder + "/ips")
		if err != nil {
			log.Fatalf("Error reading /ips file: %v", err)
		}
	}

	simpleheaders, err := parseFile(folder + "/simpleheaders")
	if err != nil {
		log.Fatalf("Error reading /simpleheaders file: %v", err)
	}

	w := goccm.New(max_goroutines)

	results := []Result{}

	for _, ip := range ips {
		for _, line := range lines {
			time.Sleep(time.Duration(delay) * time.Millisecond)
			w.Wait()
			go func(line, ip string) {
				headers := append(headers, header{line, ip})

				statusCode, response, err := request(method, uri, headers, proxy)

				if err != nil {
					log.Println(err)
				}

				results = append(results, Result{line + ": " + ip, statusCode, len(response)})
				w.Done()
			}(line, ip)
		}
	}

	for _, simpleheader := range simpleheaders {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			x := strings.Split(line, " ")
			headers := append(headers, header{x[0], x[1]})

			statusCode, response, err := request(method, uri, headers, proxy)
			if err != nil {
				log.Println(err)
			}

			results = append(results, Result{x[0] + ": " + x[1], statusCode, len(response)})
			w.Done()
		}(simpleheader)
	}
	w.WaitAllDone()
	printResponse(results)
}

// requestEndPaths makes HTTP requests using a list of custom end paths from a file and prints the results.
func requestEndPaths(uri string, headers []header, proxy *url.URL, folder string, method string) {
	color.Cyan("\n[####] CUSTOM PATHS [####]")

	var lines []string
	lines, err := parseFile(folder + "/endpaths")
	if err != nil {
		log.Fatalf("Error reading custom paths file: %v", err)
	}

	w := goccm.New(max_goroutines)

	results := []Result{}

	for _, line := range lines {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			statusCode, response, err := request(method, uri+line, headers, proxy)
			if err != nil {
				log.Println(err)
			}

			results = append(results, Result{uri + line, statusCode, len(response)})
			w.Done()
		}(line)
	}
	w.WaitAllDone()
	printResponse(results)
}

// requestMidPaths makes HTTP requests using a list of custom mid paths from a file and prints the results.
func requestMidPaths(uri string, headers []header, proxy *url.URL, folder string, method string) {
	var lines []string
	lines, err := parseFile(folder + "/midpaths")
	if err != nil {
		log.Fatalf("Error reading custom paths file: %v", err)
	}

	x := strings.Split(uri, "/")
	var uripath string

	if uri[len(uri)-1:] == "/" {
		uripath = x[len(x)-2]
	} else {
		uripath = x[len(x)-1]
	}

	baseuri := strings.ReplaceAll(uri, uripath, "")
	baseuri = baseuri[:len(baseuri)-1]

	w := goccm.New(max_goroutines)

	results := []Result{}

	for _, line := range lines {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			var fullpath string
			if uri[len(uri)-1:] == "/" {
				fullpath = baseuri + line + uripath + "/"
			} else {
				fullpath = baseuri + "/" + line + uripath
			}

			statusCode, response, err := request(method, fullpath, headers, proxy)
			if err != nil {
				log.Println(err)
			}

			results = append(results, Result{fullpath, statusCode, len(response)})
			w.Done()
		}(line)
	}
	w.WaitAllDone()
	printResponse(results)
}

// requestCapital makes HTTP requests by capitalizing each letter in the last part of the URI and prints the results.
func requestCapital(uri string, headers []header, proxy *url.URL, method string) {
	color.Cyan("\n[####] CAPITALIZATION [####]")

	x := strings.Split(uri, "/")
	var uripath string

	if uri[len(uri)-1:] == "/" {
		uripath = x[len(x)-2]
	} else {
		uripath = x[len(x)-1]
	}
	baseuri := strings.ReplaceAll(uri, uripath, "")
	baseuri = baseuri[:len(baseuri)-1]

	w := goccm.New(max_goroutines)

	results := []Result{}

	for _, z := range uripath {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(z rune) {
			newpath := strings.Map(func(r rune) rune {
				if r == z {
					return unicode.ToUpper(r)
				} else {
					return r
				}
			}, uripath)

			var fullpath string
			if uri[len(uri)-1:] == "/" {
				fullpath = baseuri + newpath + "/"
			} else {
				fullpath = baseuri + "/" + newpath
			}

			statusCode, response, err := request(method, fullpath, headers, proxy)
			if err != nil {
				log.Println(err)
			}

			results = append(results, Result{fullpath, statusCode, len(response)})
			w.Done()
		}(z)
	}
	w.WaitAllDone()
	printResponse(results)
}

// requester is the main function that runs all the tests.
func requester(uri string, proxy string, userAgent string, req_headers []string, bypassIp string, folder string, method string, verbose bool) {
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
	if len(userAgent) == 0 {
		userAgent = "dontgo403"
	}
	// Set default request method to GET.
	if len(method) == 0 {
		method = "GET"
	}

	headers := []header{
		{"User-Agent", userAgent},
	}

	// Parse custom headers from CLI arguments and add them to the headers slice.
	if len(req_headers[0]) != 0 {
		for _, _header := range req_headers {
			header_split := strings.Split(_header, ":")
			headers = append(headers, header{header_split[0], header_split[1]})
		}
	}

	_verbose = verbose

	// Call each function that will send HTTP requests with different variations of headers and URLs.
	requestDefault(uri, headers, userProxy, method)
	requestMethods(uri, headers, userProxy, folder)
	requestHeaders(uri, headers, userProxy, bypassIp, folder, method)
	requestEndPaths(uri, headers, userProxy, folder, method)
	requestMidPaths(uri, headers, userProxy, folder, method)
	requestCapital(uri, headers, userProxy, method)
}
