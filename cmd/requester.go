
package cmd

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/fatih/color"
)

func printResponse(ch1 chan string, ch2 chan int){
	for e := range ch1 {
		fmt.Printf("%s: ", e)
		code := <-ch2
		switch code {
		case 200, 201, 202, 203, 204, 205, 206:
			color.Green(strconv.Itoa(code))
		case 300, 301, 302, 303, 304, 307, 308:
			color.Yellow(strconv.Itoa(code))
		case 400, 401, 402, 403, 404, 405, 406, 407, 408, 413:
			color.Red(strconv.Itoa(code))
		case 500, 501, 502, 503, 504, 505, 511:
			color.Magenta(strconv.Itoa(code))
		}
	}
}

func requestMethods(uri string, proxy *url.URL, useragent string){
	ch1 := make(chan string)
	ch2 := make(chan int)

	color.Cyan("\n[+] HTTP METHODS")
	file, err := os.Open("payloads/httpmethods")
	if err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	var txtlines []string

	for scanner.Scan(){
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
			client := &http.Client{}

			if len(proxy.Host) != 0 {
				client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
			}

			req, err := http.NewRequest(line, uri, nil)
			req.Header.Add("User-Agent", useragent)
			resp, err := client.Do(req)
			if err != nil {
				log.Fatal(err)
			}

			ch1 <- line
			ch2 <- resp.StatusCode
		}(line)

	}

	go printResponse(ch1, ch2)
	wg.Wait()
}


func requestHeaders(uri string, proxy *url.URL, useragent string) {
	ch1 := make(chan string)
	ch2 := make(chan int)

	color.Cyan("\n[+] VERB TAMPERING")
	file, err := os.Open("payloads/headers")
	if err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	var txtlines []string

	for scanner.Scan(){
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
			client := &http.Client{}

			if len(proxy.Host) != 0 {
				client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
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

			ch1 <- line
			ch2 <- resp.StatusCode
		}(line)
	}

	go printResponse(ch1, ch2)
	wg.Wait()
}

func requestEndPaths(uri string, proxy *url.URL, useragent string) {
	ch1 := make(chan string)
	ch2 := make(chan int)

	color.Cyan("\n[+] CUSTOM PATHS")
	file, err := os.Open("payloads/endpaths")
	if err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	var txtlines []string

	for scanner.Scan(){
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
			client := &http.Client{}

			if len(proxy.Host) != 0 {
				client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
			}

			req, err := http.NewRequest("GET", uri+line, nil)
			req.Header.Add("User-Agent", useragent)

			resp, err := client.Do(req)
			if err != nil {
				log.Fatal(err)
			}

			lineprint := "End path " + line
			ch1 <- lineprint
			ch2 <- resp.StatusCode
		}(line)
	}

	go printResponse(ch1, ch2)
	wg.Wait()
}

func requestMidPaths(uri string, proxy *url.URL, useragent string) {
	ch1 := make(chan string)
	ch2 := make(chan int)

	file, err := os.Open("payloads/midpaths")
	if err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	var txtlines []string

	for scanner.Scan(){
		txtlines = append(txtlines, scanner.Text())
	}

	err = file.Close()
	if err != nil {
		log.Fatal(err)
	}

	h := strings.Split(uri, "/")
	uripath := h[3]
	baseuri := strings.ReplaceAll(uri, uripath, "")

	var wg sync.WaitGroup
	wg.Add(len(txtlines))

	for _, line := range txtlines {
		go func(line string) {
			defer wg.Done()
			client := &http.Client{}

			if len(proxy.Host) != 0 {
				client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
			}

			req, err := http.NewRequest("GET", baseuri+line+uripath, nil)
			req.Header.Add("User-Agent", useragent)

			resp, err := client.Do(req)
			if err != nil {
				log.Fatal(err)
			}

			lineprint := "Mid path " + line
			ch1 <- lineprint
			ch2 <- resp.StatusCode
		}(line)
	}

	go printResponse(ch1, ch2)
	wg.Wait()
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
		useragent = "dontgo403/0.1"
	}
	requestMethods(uri, userProxy, useragent)
	requestHeaders(uri, userProxy, useragent)
	requestEndPaths(uri, userProxy, useragent)
	requestMidPaths(uri, userProxy, useragent)
}