package cmd

import (
	"bufio"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

// Reads a file given its filename and returns a list containing each of its lines.
func parseFile(filename string) (lines []string, err error) {

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	err = file.Close()
	if err != nil {
		return nil, err
	}

	return lines, nil
}

type header struct {
	key   string
	value string
}

// Makes a GET request using headers `headers` proxy `proxy`.
//
// Uses a custom method if specified.
func request(method, uri string, headers []header, proxy *url.URL) (statusCode int, response []byte, err error) {

	if method == "" { // check if it is nil
		method = "GET"
	}

	var _proxy *url.URL
	if len(proxy.Host) != 0 {
		_proxy = proxy
	} else {
		_proxy = nil
	}

	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(_proxy),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: (&net.Dialer{Timeout: 6 * time.Second}).DialContext}}

	req, err := http.NewRequest(method, uri, nil)
	if err != nil {
		return 0, nil, err
	}

	for _, header := range headers {
		req.Header.Add(header.key, header.value)
	}

	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}

	resp, err := httputil.DumpResponse(res, true)
	if err != nil {
		return 0, nil, err
	}

	return res.StatusCode, resp, nil

}
