<p align="center">
  <img src="https://i.imgur.com/NtlwDVT.png" width="600" height="200">
</p>

<h1 align="center">NoMore403</h1>

<p align="center">
  <a href="https://github.com/devploit/nomore403/issues"><img alt="contributions welcome" src="https://img.shields.io/badge/contributions-welcome-brightgreen.svg?style=flat"></a>
</p>

## Introduction

`nomore403` is an innovative tool designed to help cybersecurity professionals and enthusiasts bypass HTTP 40X errors encountered during web security assessments. Unlike other solutions, `nomore403` automates various techniques to seamlessly navigate past these access restrictions, offering a broad range of strategies from header manipulation to method tampering.

## Prerequisites

Before you install and run `nomore403`, make sure you have the following:
- Go 1.15 or higher installed on your machine.

## Installation

### From Releases

Grab the latest release for your OS from our [Releases](https://github.com/devploit/nomore403/releases) page.

### Compile from Source

If you prefer to compile the tool yourself:

```bash
git clone https://github.com/devploit/nomore403
cd nomore403
go get
go build
```

## Customization

To edit or add new bypasses, modify the payloads directly in the [payloads](https://github.com/devploit/nomore403/tree/main/payloads) folder. nomore403 will automatically incorporate these changes.

## Usage

### Output example

```bash
    ________  ________  ________  ________  ________  ________  ________  ________  ________
   ╱     ╱  ╲╱        ╲╱    ╱   ╲╱        ╲╱        ╲╱        ╲╱    ╱   ╲╱        ╲╱__      ╲
  ╱         ╱    ╱    ╱         ╱    ╱    ╱    ╱    ╱       __╱         ╱    ╱    ╱__       ╱
 ╱         ╱         ╱         ╱         ╱        _╱       __/____     ╱         ╱         ╱
 ╲__╱_____╱╲________╱╲__╱__╱__╱╲________╱╲____╱___╱╲________╱    ╱____╱╲________╱╲________╱  

Target: 		https://domain.com/admin
Headers:                false
Proxy:                  false
User Agent:             Mozilla/5.0 (compatible; MSIE 10.0; Windows NT 6.2; WOW64; Trident/7.0; 1ButtonTaskbar)
Method:                 GET
Payloads folder:        payloads
Custom bypass IP:       false
Follow Redirects:       false
Rate Limit detection:   false
Verbose:                false

━━━━━━━━━━━━━ DEFAULT REQUEST ━━━━━━━━━━━━━
403 	  429 bytes https://domain.com/admin

━━━━━━━━━━━━━ VERB TAMPERING ━━━━━━━━━━━━━━

━━━━━━━━━━━━━ HEADERS ━━━━━━━━━━━━━━━━━━━━━

━━━━━━━━━━━━━ CUSTOM PATHS ━━━━━━━━━━━━━━━━
200 	 2047 bytes https://domain.com/;///..admin

━━━━━━━━━━━━━ HTTP VERSIONS ━━━━━━━━━━━━━━━
403      429 bytes HTTP/1.0
403      429 bytes HTTP/1.1
403      429 bytes HTTP/2

━━━━━━━━━━━━━ CASE SWITCHING ━━━━━━━━━━━━━━
200 	 2047 bytes https://domain.com/%61dmin
```

### Basic Usage

```bash
./nomore403 -u https://domain.com/admin
```

### Verbose Mode + Proxy + Specific techniques to use

```bash
./nomore403 -u https://domain.com/admin -x http://127.0.0.1:8080 -k headers,http-versions -v
```

### Parse request from Burp

```bash
./nomore403 --request-file request.txt
```

### Use custom header + specific IP address for bypasses

```bash
./nomore403 -u https://domain.com/admin -H "Environment: Staging" -b 8.8.8.8
```

### Set new max of goroutines + add delay between requests
```bash
./nomore403 -u https://domain.com/admin -m 10 -d 200
```

## Options

```bash
./nomore403 -h
Command line application that automates different ways to bypass 40X codes.

Usage:
  nomore403 [flags]

Flags:
  -i, --bypass-ip string      Use a specified IP address or hostname for bypassing access controls. Injects this IP in headers like 'X-Forwarded-For'.
  -d, --delay int             Specify a delay between requests in milliseconds. Helps manage request rate (default: 0ms).
  -f, --folder string         Specify the folder location for payloads if not in the same directory as the executable.
  -H, --header strings        Add one or more custom headers to requests. Repeatable flag for multiple headers.
  -h, --help                  help for nomore403
      --http                  Use HTTP instead of HTTPS for requests defined in the request file.
  -t, --http-method string    Specify the HTTP method for the request (e.g., GET, POST). Default is 'GET'.
  -m, --max-goroutines int    Limit the maximum number of concurrent goroutines to manage load (default: 50). (default 50)
      --no-banner             Disable the display of the startup banner (default: banner shown).
  -x, --proxy string          Specify a proxy server for requests, e.g., 'http://server:port'.
      --random-agent          Enable the use of a randomly selected User-Agent.
  -l, --rate-limit            Halt requests upon encountering a 429 (rate limit) HTTP status code.
  -r, --redirect              Automatically follow redirects in responses.
      --request-file string   Load request configuration and flags from a specified file.
  -k, --technique strings     Specify one or more attack techniques to use (e.g., headers,path-case). (default [verbs,verbs-case,headers,endpaths,midpaths,http-versions,path-case])
      --timeout int           Specify a max timeout time in ms (default 6000)
  -u, --uri string            Specify the target URL for the request.
  -a, --user-agent string     pecify a custom User-Agent string for requests (default: 'nomore403').
  -v, --verbose               Enable verbose output for detailed request/response logging.
```

## Contributing

We welcome contributions of all forms. Here's how you can help:

 - Report bugs and suggest features.
 - Submit pull requests with bug fixes and new features.

## Security Considerations

While nomore403 is designed for educational and ethical testing purposes, it's important to use it responsibly and with permission on target systems. Please adhere to local laws and guidelines.

## License

nomore403 is released under the MIT License. See the [LICENSE](https://github.com/devploit/dontgo403/blob/main/LICENSE) file for details.

## Contact

[![Twitter: devploit](https://img.shields.io/badge/-Twitter-blue?style=flat-square&logo=Twitter&logoColor=white&link=https://twitter.com/devploit/)](https://twitter.com/devploit/)
