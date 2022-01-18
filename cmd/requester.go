package cmd

import (
	"bufio"
	"crypto/tls"
	"fmt"
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

type Result struct {
	line string
	code int
}

func printResponse(line string, code int) {
	switch code {
	case 200, 201, 202, 203, 204, 205, 206:
		fmt.Printf("%s: %s\n", line, color.GreenString(strconv.Itoa(code)))
	case 300, 301, 302, 303, 304, 307, 308:
		fmt.Printf("%s: %s\n", line, color.YellowString(strconv.Itoa(code)))
	case 400, 401, 402, 403, 404, 405, 406, 407, 408, 413:
		fmt.Printf("%s: %s\n", line, color.RedString(strconv.Itoa(code)))
	case 500, 501, 502, 503, 504, 505, 511:
		fmt.Printf("%s: %s\n", line, color.MagentaString(strconv.Itoa(code)))
	}
}

func requestMethods(uri string, proxy *url.URL, useragent string) {
	ch1 := make(chan Result)

	color.Cyan("\n[+] HTTP METHODS")
	file, err := os.Open("payloads/httpmethods")
	if err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	var txtlines []string

	for scanner.Scan() {
		txtlines = append(txtlines, scanner.Text())
	}

	err = file.Close()
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(len(txtlines))

	for _, line := range txtlines {
		go func(line string) {
			defer wg.Done()
			client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}}

			if len(proxy.Host) != 0 {
				client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy),
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}}
			}

			req, err := http.NewRequest(line, uri, nil)
			req.Header.Add("User-Agent", useragent)
			resp, err := client.Do(req)
			if err != nil {
				log.Fatal(err)
			}

			printResponse(line, resp.StatusCode)
		}(line)
	}
	wg.Wait()
	close(ch1)
}

func requestHeaders(uri string, proxy *url.URL, useragent string) {
	ch1 := make(chan Result)

	color.Cyan("\n[+] VERB TAMPERING")
	file, err := os.Open("payloads/headers")
	if err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	var txtlines []string

	for scanner.Scan() {
		txtlines = append(txtlines, scanner.Text())
	}

	err = file.Close()
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(len(txtlines))

	for _, line := range txtlines {
		go func(line string) {
			defer wg.Done()
			client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}}

			if len(proxy.Host) != 0 {
				client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy),
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}}
			}

			req, err := http.NewRequest("GET", uri, nil)
			req.Header.Add("User-Agent", useragent)

			h := strings.Split(line, " ")
			header, value := h[0], h[1]

			req.Header.Add(header, value)
			resp, err := client.Do(req)
			if err != nil {
				log.Fatal(err)
			}

			printResponse(line, resp.StatusCode)
		}(line)
	}
	wg.Wait()
	close(ch1)
}

func requestEndPaths(uri string, proxy *url.URL, useragent string) {
	ch1 := make(chan Result)

	color.Cyan("\n[+] CUSTOM PATHS")
	file, err := os.Open("payloads/endpaths")
	if err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	var txtlines []string

	for scanner.Scan() {
		txtlines = append(txtlines, scanner.Text())
	}

	err = file.Close()
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(len(txtlines))

	for _, line := range txtlines {
		go func(line string) {
			defer wg.Done()
			client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}}

			if len(proxy.Host) != 0 {
				client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy),
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}}
			}

			req, err := http.NewRequest("GET", uri+line, nil)
			req.Header.Add("User-Agent", useragent)

			resp, err := client.Do(req)
			if err != nil {
				log.Fatal(err)
			}

			lineprint := "End path " + line
			printResponse(lineprint, resp.StatusCode)
		}(line)
	}
	wg.Wait()
	close(ch1)
}

func requestMidPaths(uri string, proxy *url.URL, useragent string) {
	ch1 := make(chan Result)

	file, err := os.Open("payloads/midpaths")
	if err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	var txtlines []string

	for scanner.Scan() {
		txtlines = append(txtlines, scanner.Text())
	}

	err = file.Close()
	if err != nil {
		log.Fatal(err)
	}

	h := strings.Split(uri, "/")
	var uripath string

	if uri[len(uri)-1:] == "/" {
		uripath = h[len(h)-2]
	} else {
		uripath = h[len(h)-1]
	}

	baseuri := strings.ReplaceAll(uri, uripath, "")
	baseuri = baseuri[:len(baseuri)-1]

	var wg sync.WaitGroup
	wg.Add(len(txtlines))

	for _, line := range txtlines {
		go func(line string) {
			defer wg.Done()
			client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}}

			if len(proxy.Host) != 0 {
				client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy),
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}}
			}
			var fullpath string

			if uri[len(uri)-1:] == "/" {
				fullpath = baseuri + line + uripath + "/"
			} else {
				fullpath = baseuri + "/" + line + uripath
			}

			req, err := http.NewRequest("GET", fullpath, nil)
			req.Header.Add("User-Agent", useragent)

			resp, err := client.Do(req)
			if err != nil {
				log.Fatal(err)
			}

			lineprint := "Mid path " + line
			printResponse(lineprint, resp.StatusCode)
		}(line)
	}
	wg.Wait()
	close(ch1)
}

func requestCapital(uri string, proxy *url.URL, useragent string) {
	ch1 := make(chan Result)

	color.Cyan("\n[+] CAPITALIZATION")

	h := strings.Split(uri, "/")
	var uripath string

	if uri[len(uri)-1:] == "/" {
		uripath = h[len(h)-2]
	} else {
		uripath = h[len(h)-1]
	}
	baseuri := strings.ReplaceAll(uri, uripath, "")
	baseuri = baseuri[:len(baseuri)-1]

	var wg sync.WaitGroup
	wg.Add(len(uripath))

	for _, z := range uripath {
		go func(z string) {
			defer wg.Done()
			client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}}

			if len(proxy.Host) != 0 {
				client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy),
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}}
			}
			newpath := strings.ReplaceAll(uripath, string(z), strings.ToUpper(string(z)))
			var fullpath string

			if uri[len(uri)-1:] == "/" {
				fullpath = baseuri + newpath + "/"
			} else {
				fullpath = baseuri + "/" + newpath
			}

			req, err := http.NewRequest("GET", fullpath, nil)
			req.Header.Add("User-Agent", useragent)

			resp, err := client.Do(req)
			if err != nil {
				log.Fatal(err)
			}

			lineprint := fullpath
			printResponse(lineprint, resp.StatusCode)
		}(string(z))
	}
	wg.Wait()
	close(ch1)
}

func requester(uri string, proxy string, useragent string) {
	if len(proxy) != 0 {
		if strings.Contains(proxy, "http") != true {
			proxy = "http://" + proxy
		}
		color.Magenta("\n[*] USING PROXY: %s\n", proxy)
	}
	userProxy, _ := url.Parse(proxy)
	h := strings.Split(uri, "/")
	if len(h) < 4 {
		uri += "/"
	}
	if len(useragent) == 0 {
		useragent = "dontgo403/0.2"
	}
	requestMethods(uri, userProxy, useragent)
	requestHeaders(uri, userProxy, useragent)
	requestEndPaths(uri, userProxy, useragent)
	requestMidPaths(uri, userProxy, useragent)
	requestCapital(uri, userProxy, useragent)
}
