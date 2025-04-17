<p align="center">
  <img src="https://i.imgur.com/F4D1zhr.png" width="350" height="200" alt="logo">
</p>

<h1 align="center">NoMore403</h1>

<p align="center">
  <a href="https://github.com/devploit/nomore403/issues"><img alt="contributions welcome" src="https://img.shields.io/badge/contributions-welcome-brightgreen.svg?style=flat"></a>
</p>

## Table of Contents
- [Introduction](#introduction)
- [Features](#features)
- [Implemented Bypass Techniques](#implemented-bypass-techniques)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [How It Works](#how-it-works)
- [Customization](#customization)
- [Usage](#usage)
- [Options](#options)
- [Common Use Cases](#common-use-cases)
- [Contributing](#contributing)
- [Security Considerations](#security-considerations)
- [License](#license)
- [Acknowledgments](#acknowledgments)
- [Contact](#contact)

## Introduction

`nomore403` is an innovative tool designed to help cybersecurity professionals and enthusiasts bypass HTTP 40X errors encountered during web security assessments. Unlike other solutions, `nomore403` automates various techniques to seamlessly navigate past these access restrictions, offering a broad range of strategies from header manipulation to method tampering.

## Features

- **Auto-calibration**: Automatically detects server base responses to identify successful bypasses
- **Multiple bypass techniques**: Implements 8 different techniques to bypass restrictions
- **High concurrency**: Uses goroutines for fast and efficient testing
- **Customizable**: Easily add new payloads and techniques

## Implemented Bypass Techniques

- **Verb Tampering**: Tests different HTTP methods to access protected resources
- **Verb Case Switching**: Manipulates HTTP method capitalization to detect incorrect implementations
- **Headers**: Injects headers designed for bypassing like X-Forwarded-For, X-Original-URL, etc.
- **Custom Paths**: Tests alternative paths that can bypass access restrictions
- **Path Traversal (midpaths)**: Inserts patterns in the middle of paths to confuse parsers
- **Double-Encoding**: Uses double URL encoding to evade filters
- **HTTP Versions**: Tests different HTTP versions (1.0, 1.1) to identify inconsistent behaviors
- **Path Case Switching**: Manipulates uppercase/lowercase in paths to detect case-sensitive configurations

## Prerequisites

Before you install and run `nomore403`, make sure you have the following:
- Go 1.19 or higher installed on your machine.

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

## How It Works

1. **Auto-calibration**: The tool makes a request to a non-existent path to determine the base response
2. **Default request**: Makes a standard request to the target for comparison
3. **Technique application**: Executes selected techniques in parallel
4. **Result filtering**: Shows only responses that differ from the initial calibration (unless verbose mode is used)

## Customization

To edit or add new bypasses, modify the payloads directly in the [payloads](https://github.com/devploit/nomore403/tree/main/payloads) folder. nomore403 will automatically incorporate these changes.

### Payloads Folder Structure

- **headers**: Headers used for bypassing
- **ips**: IP addresses to inject in specific headers
- **httpmethods**: Alternative HTTP methods
- **endpaths**: Custom paths to add at the end of the target URL
- **midpaths**: Patterns to insert in the middle of paths
- **simpleheaders**: Common simple headers
- **useragents**: List of User-Agents for rotation

## Usage

### Output example

```bash
━━━━━━━━━━━━━━ NOMORE403 CONFIGURATION ━━━━━━━━━━━━━━━━━━
Target:                 https://domain.com/admin
Headers:                false
Proxy:                  false
User Agent:             nomore403
Method:                 GET
Payloads folder:        payloads
Custom bypass IP:       false
Follow Redirects:       false
Rate Limit detection:   false
Status:                 
Timeout (ms):           6000
Delay (ms):             0
Techniques:             verbs, verbs-case, headers, endpaths, midpaths, double-encoding, http-versions, path-case
Unique:                 false
Verbose:                false

━━━━━━━━━━━━━━━ AUTO-CALIBRATION RESULTS ━━━━━━━━━━━━━━━
[✔] Calibration URI: https://domain.com/admin/calibration_test_123456
[✔] Status Code: 404
[✔] Content Length: 1821 bytes

━━━━━━━━━━━━━ DEFAULT REQUEST ━━━━━━━━━━━━━
403 	  429 bytes https://domain.com/admin

━━━━━━━━━━━━━ VERB TAMPERING ━━━━━━━━━━━━━━

━━━━━ VERB TAMPERING CASE SWITCHING ━━━━━━━

━━━━━━━━━━━━━ HEADERS ━━━━━━━━━━━━━━━━━━━━━

━━━━━━━━━━━━━ CUSTOM PATHS ━━━━━━━━━━━━━━━━
200 	 2047 bytes https://domain.com/;///..admin

━━━━━━━━━━━━━ DOUBLE-ENCODING ━━━━━━━━━━━━━

━━━━━━━━━━━━━ HTTP VERSIONS ━━━━━━━━━━━━━━━
403      429 bytes HTTP/1.0

━━━━━━━━━━ PATH CASE SWITCHING ━━━━━━━━━━━━
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
./nomore403 -u https://domain.com/admin -H "Environment: Staging" -i 8.8.8.8
```

### Set new max of goroutines + add delay between requests
```bash
./nomore403 -u https://domain.com/admin -m 10 -d 200
```

### Filter by specific status codes
```bash
./nomore403 -u https://domain.com/admin --status 200,302
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
  -x, --proxy string          Specify a proxy server for requests (e.g., 'http://server:port').
      --random-agent          Enable the use of a randomly selected User-Agent.
  -l, --rate-limit            Halt requests upon encountering a 429 (rate limit) HTTP status code.
  -r, --redirect              Automatically follow redirects in responses.
      --request-file string   Load request configuration and flags from a specified file.
      --status strings        Filter output by comma-separated status codes (e.g., 200,301,403)
  -k, --technique strings     Specify one or more attack techniques to use (e.g., headers,path-case). (default [verbs,verbs-case,headers,endpaths,midpaths,double-encoding,http-versions,path-case])
      --timeout int           Specify a max timeout time in ms. (default 6000)
      --unique                Show unique output based on status code and response length.
  -u, --uri string            Specify the target URL for the request.
  -a, --user-agent string     Specify a custom User-Agent string for requests (default: 'nomore403').
  -v, --verbose               Enable verbose output for detailed request/response logging (not based on auto-calibrate).
```

## Common Use Cases

- **Security Audits**: Identify misconfigurations in authentication systems
- **Bug Bounty**: Discover bypasses in protected endpoints
- **Penetration Testing**: Gain access to restricted areas during assessments
- **Hardening**: Verify the robustness of implemented protections

## Contributing

We welcome contributions of all forms. Here's how you can help:

 - Report bugs and suggest features
 - Submit pull requests with bug fixes and new features
 - Add new payloads to existing folders

## Security Considerations

While nomore403 is designed for educational and ethical testing purposes, it's important to use it responsibly and with permission on target systems. Please adhere to local laws and guidelines.

## License

nomore403 is released under the MIT License. See the [LICENSE](https://github.com/devploit/dontgo403/blob/main/LICENSE) file for details.

## Acknowledgments

NoMore403 draws inspiration from several projects in the web security space:
- [Dontgo403](https://github.com/devploit/dontgo403) - The predecessor to NoMore403
- The cybersecurity community for documenting and sharing bypass techniques
- All contributors who have helped improve this tool

## Contact

[![Twitter: devploit](https://img.shields.io/badge/-Twitter-blue?style=flat-square&logo=Twitter&logoColor=white&link=https://twitter.com/devploit/)](https://twitter.com/devploit/)
