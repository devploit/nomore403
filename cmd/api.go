package cmd

import (
	"bufio"
	"crypto/tls"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

// parseFile reads a file given its filename and returns a list containing each of its lines.
func parseFile(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Fatalf("{#err}")
		}
	}(file)

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
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

// request makes an HTTP request using headers `headers` and proxy `proxy`.
//
// If `method` is empty, it defaults to "GET".
func request(method, uri string, headers []header, proxy *url.URL) (int, []byte, error) {
	if method == "" {
		method = "GET"
	}

	if len(proxy.Host) == 0 {
		proxy = nil
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxy),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			DialContext: (&net.Dialer{
				Timeout:   6 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	req, err := http.NewRequest(method, uri, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Close = true

	for _, header := range headers {
		req.Header.Add(header.key, header.value)
	}

	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Fatalf("{#err}")
		}
	}(res.Body)

	resp, err := httputil.DumpResponse(res, true)
	if err != nil {
		return 0, nil, err
	}

	return res.StatusCode, resp, nil
}

// loadFlagsFromRequestFile parse an HTTP request and configure the necessary flags for an execution
func loadFlagsFromRequestFile(requestFile string, schema bool, verbose bool) {
	// Read the content of the request file
	content, err := ioutil.ReadFile(requestFile)
	if err != nil {
		log.Fatalf("Error reading request file: %v", err)
	}
	httpSchema := "https://"

	if schema != false {
		httpSchema = "http://"
	}

	// Split the request into lines
	requestLines := strings.Split(string(content), "\n")
	firstLine := requestLines[0]
	headers := requestLines[1:]
	host := strings.Split(requestLines[1], " ")

	// Extract the HTTP method and URL from the first line of the request
	parts := strings.Split(firstLine, " ")
	uri := httpSchema + host[1] + parts[1]

	// Extract headers from the request and assign them to the req_headers slice
	var reqHeaders []string
	for _, h := range headers {
		if len(h) > 0 {
			reqHeaders = append(reqHeaders, h)
		}
	}

	// Assign the extracted values to the corresponding flag variables
	requester(uri, proxy, useragent, reqHeaders, bypassIp, folder, httpMethod, verbose, nobanner)
}
