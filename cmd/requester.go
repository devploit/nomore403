package cmd

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
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

var _verbose bool
var defaultSc int
var defaultCl int
var printMutex = &sync.Mutex{}

// printResponse prints the results of HTTP requests in a tabular format with colored output based on the status codes.
func printResponse(result Result) {
	printMutex.Lock()
	defer printMutex.Unlock()

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
	if _verbose != true {
		if ((defaultSc == result.statusCode) && (defaultCl == result.contentLength) || result.contentLength == 0 || result.statusCode == 404 || result.statusCode == 400) && result.defaultReq != true {
			return
		} else {
			fmt.Printf("%s \t%20s %s\n", code, color.BlueString(resultContentLength), result.line)
		}
	} else {
		fmt.Printf("%s \t%20s %s\n", code, color.BlueString(resultContentLength), result.line)
	}
}

func showInfo(uri string, headers []string, userAgent string, proxy *url.URL, method string, folder string, bypassIp string, verbose bool, nobanner bool) {
	if nobanner != true {
		fmt.Println(`
                                                                                       .#%%:  -#%%%*.  +#%%#+.
                                                                                      =@*#@: =@+  .%%.:+-  =@*
         :::.                             ...                                       .#@= *@: *@:   *@:  :##%@-
         :::.                             :::.             ..    -:..-.    ..   ::  =%%%%@@%:=@*. :%% =*-  :@%
 .::::::::::.   .::::::.   .:::::::::.  ::::::::.  .:::::::::.  .=::..:==-=+++:.         +#.  -*#%#+.  =*###+.
.:::....::::. .:::....:::. .::::...:::. ..::::..  ::::....:::   --::..-=+*=:.                                 
:::.     :::. :::.    .::: .:::    .:::   :::.   ::::     .:::  -=::-*#+=:                                    
::::    .:::. ::::    :::: .:::    .:::   :::.   .:::.    :::.  +-:::=::.                                     
 :::::.:::::.  ::::..::::  .:::    .:::   .:::.:. .:::::::::.  .+=:::::.                                      
  ..::::.:::    ..::::..   .:::     :::    ..:::.   .....::::.   -=:.::                                       
                                                 .:::     ::::                                                
                                                  :::::..::::.                                                
	`)
	}
	fmt.Printf("%s \t\t%s\n", "Target:", uri)
	if len(headers[0]) != 0 {
		for _, header := range headers {
			fmt.Printf("%s \t\t%s\n", "Headers:", header)
		}
	} else {
		fmt.Printf("%s \t\t%s\n", "Headers:", "false")
	}
	if len(proxy.Host) != 0 {
		fmt.Printf("%s \t\t\t%s\n", "Proxy:", proxy.Host)
	} else {
		fmt.Printf("%s \t\t\t%s\n", "Proxy:", "false")
	}
	fmt.Printf("%s \t\t%s\n", "User Agent:", userAgent)
	fmt.Printf("%s \t\t%s\n", "Method:", method)
	fmt.Printf("%s \t%s\n", "Payloads folder:", folder)
	if len(bypassIp) != 0 {
		fmt.Printf("%s \t%s\n", "Custom bypass IP:", bypassIp)
	} else {
		fmt.Printf("%s \t%s\n", "Custom bypass IP:", "false")
	}
	fmt.Printf("%s \t\t%t\n", "Verbose:", verbose)
}

// requestDefault makes HTTP request to check the default response
func requestDefault(uri string, headers []header, proxy *url.URL, method string) {
	color.Cyan("\n━━━━━━━━━━━━━ DEFAULT REQUEST ━━━━━━━━━━━━━")

	var results []Result

	statusCode, response, err := request(method, uri, headers, proxy)
	if err != nil {
		log.Println(err)
	}

	results = append(results, Result{method, statusCode, len(response), true})
	printResponse(Result{uri, statusCode, len(response), true})
	for _, result := range results {
		defaultSc = result.statusCode
		defaultCl = result.contentLength
	}
}

// requestMethods makes HTTP requests using a list of methods from a file and prints the results.
func requestMethods(uri string, headers []header, proxy *url.URL, folder string) {
	color.Cyan("\n━━━━━━━━━━━━━ VERB TAMPERING ━━━━━━━━━━━━━━")

	var lines []string
	lines, err := parseFile(folder + "/httpmethods")
	if err != nil {
		log.Fatalf("Error reading /httpmethods file: %v", err)
	}

	w := goccm.New(maxGoroutines)

	for _, line := range lines {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			statusCode, response, err := request(line, uri, headers, proxy)
			if err != nil {
				log.Println(err)
			}

			printResponse(Result{line, statusCode, len(response), false})
			w.Done()
		}(line)
	}
	w.WaitAllDone()
}

// requestHeaders makes HTTP requests using a list of headers from a file and prints the results. It can also bypass IP address restrictions by specifying a bypass IP address.
func requestHeaders(uri string, headers []header, proxy *url.URL, bypassIp string, folder string, method string) {
	color.Cyan("\n━━━━━━━━━━━━━ HEADERS ━━━━━━━━━━━━━━━━━━━━━")

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

	w := goccm.New(maxGoroutines)

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

				printResponse(Result{line, statusCode, len(response), false})
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

			printResponse(Result{line, statusCode, len(response), false})
			w.Done()
		}(simpleheader)
	}
	w.WaitAllDone()
}

// requestEndPaths makes HTTP requests using a list of custom end paths from a file and prints the results.
func requestEndPaths(uri string, headers []header, proxy *url.URL, folder string, method string) {
	color.Cyan("\n━━━━━━━━━━━━━ CUSTOM PATHS ━━━━━━━━━━━━━━━━")

	var lines []string
	lines, err := parseFile(folder + "/endpaths")
	if err != nil {
		log.Fatalf("Error reading custom paths file: %v", err)
	}

	w := goccm.New(maxGoroutines)

	for _, line := range lines {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(line string) {
			statusCode, response, err := request(method, uri+line, headers, proxy)
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
func requestMidPaths(uri string, headers []header, proxy *url.URL, folder string, method string) {
	var lines []string
	lines, err := parseFile(folder + "/midpaths")
	if err != nil {
		log.Fatalf("Error reading custom paths file: %v", err)
	}

	x := strings.Split(uri, "/")
	var uripath string

	parsedURL, err := url.Parse(uri)
	if parsedURL.Path != "" && parsedURL.Path != "/" {
		if uri[len(uri)-1:] == "/" {
			uripath = x[len(x)-2]
		} else {
			uripath = x[len(x)-1]
		}

		baseuri := strings.ReplaceAll(uri, uripath, "")
		baseuri = baseuri[:len(baseuri)-1]

		w := goccm.New(maxGoroutines)

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

				printResponse(Result{fullpath, statusCode, len(response), false})
				w.Done()
			}(line)
		}
		w.WaitAllDone()
	}
}

// requestCaseSwitching makes HTTP requests by capitalizing each letter in the last part of the URI and try to use URL encoded characters.
func requestCaseSwitching(uri string, headers []header, proxy *url.URL, method string) {
	color.Cyan("\n━━━━━━━━━━━━━ CASE SWITCHING ━━━━━━━━━━━━━━")

	parsedURL, err := url.Parse(uri)
	if err != nil {
		log.Println(err)
		return
	}

	baseuri := parsedURL.Scheme + "://" + parsedURL.Host
	uripath := strings.Trim(parsedURL.Path, "/")

	if len(uripath) == 0 {
		os.Exit(0)
	}

	w := goccm.New(maxGoroutines)

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

			printResponse(Result{fullpath, statusCode, len(response), false})
			w.Done()
		}(z)
	}

	for _, z := range uripath {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.Wait()
		go func(z rune) {
			encodedChar := fmt.Sprintf("%%%X", z) // convert rune to its hexadecimal ASCII value
			newpath := strings.Replace(uripath, string(z), encodedChar, 1)

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

			printResponse(Result{fullpath, statusCode, len(response), false})
			w.Done()
		}(z)
	}
	w.WaitAllDone()
}

// requester is the main function that runs all the tests.
func requester(uri string, proxy string, userAgent string, reqHeaders []string, bypassIp string, folder string, method string, verbose bool, banner bool) {
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
	if len(reqHeaders[0]) != 0 {
		for _, _header := range reqHeaders {
			headerSplit := strings.Split(_header, ":")
			headers = append(headers, header{headerSplit[0], headerSplit[1]})
		}
	}

	_verbose = verbose

	// Call each function that will send HTTP requests with different variations of headers and URLs.
	showInfo(uri, reqHeaders, userAgent, userProxy, method, folder, bypassIp, verbose, banner)
	requestDefault(uri, headers, userProxy, method)
	requestMethods(uri, headers, userProxy, folder)
	requestHeaders(uri, headers, userProxy, bypassIp, folder, method)
	requestEndPaths(uri, headers, userProxy, folder, method)
	requestMidPaths(uri, headers, userProxy, folder, method)
	requestCaseSwitching(uri, headers, userProxy, method)
}
