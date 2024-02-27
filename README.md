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

Target: 		        https://domain.com/admin
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

### Verbose Mode + Proxy

```bash
./nomore403 -u https://domain.com/admin -x http://127.0.0.1:8080 -v
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
  -i, --bypass-ip string      Try bypass tests with a specific IP address (or hostname). For example: 'X-Forwarded-For: 192.168.0.1' instead of 'X-Forwarded-For: 127.0.0.1'
  -d, --delay int             Set a delay (in ms) between each request (default 0ms)
  -f, --folder string         Define payloads folder (if its not in the same path as binary)
  -H, --header strings        Add a custom header to the requests (can be specified multiple times)
  -h, --help                  help for nomore403
      --http                  Set HTTP schema for request-file requests (default HTTPS)
  -t, --http-method string    HTTP method to use (default 'GET')
  -m, --max-goroutines int    Set the max number of goroutines working at same time (default 50)
      --no-banner             Set no-banner ON (default OFF)
  -x, --proxy string          Proxy URL. For example: http://127.0.0.1:8080
      --random-agent          Set random user-agent ON (default OFF)
  -l, --rate-limit            Stop making request if rate limit ban is detected: 429 HTTP code (default OFF)
  -r, --redirect              Set follow redirect ON (default OFF)
      --request-file string   Path to request file to load flags from
  -u, --uri string            Target URL
  -a, --user-agent string     Set the User-Agent string (default 'nomore403')
  -v, --verbose               Set verbose mode ON (default OFF)
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