<p align="center">
<img src="https://i.imgur.com/T5P5ZG0.png" width="600" height="150" >
</p>

[![contributions welcome](https://img.shields.io/badge/contributions-welcome-brightgreen.svg?style=flat)](https://github.com/dwyl/esta/issues)

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
  -b, --bypassIp string    Try bypass tests with a specific IP address (or hostname). i.e.: 'X-Forwarded-For: 192.168.0.1' instead of 'X-Forwarded-For: 127.0.0.1'
  -d, --delay int          Set a delay (in ms) between each request
  -H, --header strings     Add a custom header to the requests (can be specified multiple times)
  -h, --help               help for dontgo403
  -p, --proxy string       Proxy URL. For example: http://127.0.0.1:8080
  -u, --uri string         Target URL
  -a, --useragent string   Set the User-Agent string (default 'dontgo403/0.5')
```


### Example of usage
[![asciicast](https://asciinema.org/a/xtD6Zcx2fZj5JgRdXEUi4rdqJ.svg)](https://asciinema.org/a/xtD6Zcx2fZj5JgRdXEUi4rdqJ)


### Contact
[![Twitter: devploit](https://img.shields.io/badge/-Twitter-blue?style=flat-square&logo=Twitter&logoColor=white&link=https://twitter.com/devploit/)](https://twitter.com/devploit/)
[![Telegram: devploit](https://img.shields.io/badge/-Telegram-blue?style=flat-square&logo=Telegram&logoColor=white&link=https://t.me/devploit)](https://t.me/devploit)
