[中文简体](README.md) | English

[![Build Status](https://github.com/axetroy/forward-cli/workflows/ci/badge.svg)](https://github.com/axetroy/forward-cli/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/axetroy/forward-cli)](https://goreportcard.com/report/github.com/axetroy/forward-cli)
![Latest Version](https://img.shields.io/github/v/release/axetroy/forward-cli.svg)
![License](https://img.shields.io/github/license/axetroy/forward-cli.svg)
![Repo Size](https://img.shields.io/github/repo-size/axetroy/forward-cli.svg)

## forward-cli

A command-line tool to reverse proxy any server. eg. Github/Google/Facebook and more. [more information](https://github.com/axetroy/blog/issues/634)

![img](screenshot.png)

### Usage

```bash
forward - A command line tool to quickly setup a reverse proxy server.

USAGE:
  forward [OPTIONS] [host]

OPTIONS:
  --help                              print help information
  --version                           show version information
  --address="<string>"                specify the address that the proxy server listens on. defaults: 0.0.0.0
  --port="<int>"                      specify the port that the proxy server listens on. defaults: 80
  --proxy-external                    whether to proxy external host. defaults: false
  --proxy-external-ignore=<host>      specify the external host without using a proxy. defaults: ""
  --req-header="key=value"            specify the request header attached to the request. Allow multiple flags. defaults: ""
  --res-header="key=value"            specify the response headers. Allow multiple flags. defaults: ""
  --cors                              whether enable cors. defaults: false
  --overwrite=<folder>                enable overwrite with a folder. defaults: ""
  --no-cache                          disabled cache for response. defaults: true
  --tls-cert-file=<filepath>          the cert file path for enabled tls. defaults: ""
  --tls-key-file=<filepath>           the key file path for enabled tls. defaults: ""
  --replace-content="a=b"             Contents to be replaced. defaults: ""

EXAMPLES:
  forward http://example.com
  forward --port=80 http://example.com
  forward --req-header="foo=bar" http://example.com
  forward --cors --req-header="foo=bar" --req-header="hello=world" http://example.com
  forward --tls-cert-file=/path/to/cert/file --tls-key-file=/path/to/key/file http://example.com
```

### Install

1. [Cask](https://github.com/axetroy/cask.rs)

   ```bash
   cask install github.com/axetroy/forward-cli
   ```

2 Shell (Mac/Linux)

   ```bash
   curl -fsSL https://github.com/release-lab/install/raw/v1/install.sh | bash -s -- -r=axetroy/forward-cli -e=forward
   ```

3. PowerShell (Windows):

   ```powershell
   $r="axetroy/forward-cli";$e="forward";iwr https://github.com/release-lab/install/raw/v1/install.ps1 -useb | iex
   ```

4. [Github release page](https://github.com/axetroy/forward-cli/releases) (All platforms)

   download the executable file and put the executable file to `$PATH`

5. Build and install from source using [Golang](https://golang.org) (All platforms)

   ```bash
   go install github.com/axetroy/forward-cli/cmd/forward@latest
   ```

### MISC

1. Hot to enable HTTPS?

to enable HTTPS, you need to generate the key and cert first

```bash
# generate key
openssl genrsa -out server.key 2048
# generate cert
openssl req -new -x509 -sha256 -key server.key -out server.pem -days 3650
# run proxy server
forward --tls-cert-file=server.pem --tls-key-file=server.key http://example.com
```

2. Custom proxy

```bash
# proxy https://github.com
forward https://github.com
# send request
curl http://0.0.0.0:80/api # request https://github.com/api
# send custom request
curl -H "X-Proxy-Target: https://www.google.com" http://0.0.0.0/api # request https://www.google.com/api
```

### License

The [MIT License](LICENSE)
