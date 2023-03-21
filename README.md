<p align="center">
<img src="https://i.imgur.com/T5P5ZG0.png" width="600" height="150" >
</p>

[![contributions welcome](https://img.shields.io/badge/contributions-welcome-brightgreen.svg?style=flat)](https://github.com/devploit/dontgo403/issues)

dontgo403 is a tool to bypass 40X errors.

### Installation
Grab the latest release for your OS from [RELEASES](https://github.com/devploit/dontgo403/releases)

Or compile by your own:
```bash
git clone https://github.com/devploit/dontgo403; cd dontgo403; go get; go build
```


### Customization
If you want to edit or add new bypasses, you can add it directly to the specific file in [payloads](https://github.com/devploit/dontgo403/tree/main/payloads) folder and the tool will use it.


### Options
```bash
./dontgo403 -h

Command line application that automates different ways to bypass 40X codes.

Usage:
  dontgo403 [flags]

Flags:
  -b, --bypassIp string       Try bypass tests with a specific IP address (or hostname). i.e.: 'X-Forwarded-For: 192.168.0.1' instead of 'X-Forwarded-For: 127.0.0.1'
  -d, --delay int             Set a delay (in ms) between each request. Default: 0ms
  -f, --folder string         Define payloads folder (if it's not in the same path as binary)
  -H, --header strings        Add a custom header to the requests (can be specified multiple times)
  -h, --help                  help for dontgo403
      --http                  Set HTTP schema for request-file requests (default HTTPS)
  -t, --httpMethod string     HTTP method to use (default 'GET')
  -m, --max_goroutines int    Set the max number of goroutines working at same time. Default: 50 (default 50)
  -p, --proxy string          Proxy URL. For example: http://127.0.0.1:8080
  -r, --request-file string   Path to request file to load flags from
  -u, --uri string            Target URL
  -a, --useragent string      Set the User-Agent string (default 'dontgo403')
```


### Example of usage
[![asciicast](https://asciinema.org/a/xtD6Zcx2fZj5JgRdXEUi4rdqJ.svg)](https://asciinema.org/a/xtD6Zcx2fZj5JgRdXEUi4rdqJ)


### Contact
[![Twitter: devploit](https://img.shields.io/badge/-Twitter-blue?style=flat-square&logo=Twitter&logoColor=white&link=https://twitter.com/devploit/)](https://twitter.com/devploit/)
