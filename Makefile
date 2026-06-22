.PHONY: install build

TARGET_DIR = $(HOME)/.nomore403/payloads
REPO_URL   = https://raw.githubusercontent.com/R0X4R/nomore403/main/payloads
FILES      = endpaths headers httpmethods ips midpaths simpleheaders useragents

# Dynamically resolve GOBIN using Go's environment variables.
# If GOBIN is empty, Go defaults to using GOPATH/bin.
GOBIN := $(shell go env GOBIN)
namespace :;
ifeq ($(GOBIN),)
	GOBIN := $(shell go env GOPATH)/bin
endif

install:
	@echo "[*] ENSURING GLOBAL CONFIGURATION DIRECTORIES EXIST"
	@mkdir -p $(TARGET_DIR)
	@echo "[*] DOWNLOADING ASSET PAYLOADS FROM GITHUB REPOSITORY"
	@for file in $(FILES); do \
		curl -sSL "$(REPO_URL)/$$file" -o "$(TARGET_DIR)/$$file"; \
	done
	@echo "[*] INSTALLING nomore403"
	@go install github.com/R0X4R/nomore403@latest
	@echo "[+] SUCCESS! nomore403 IS INSTALLED."
	@$(GOBIN)/nomore403 -h

build:
	@echo "[*] ENSURING GLOBAL CONFIGURATION DIRECTORIES EXIST"
	@mkdir -p $(TARGET_DIR)
	@echo "[*] DEPLOYING LOCAL PAYLOADS"
	@cp -r payloads/* $(TARGET_DIR)/
	@echo "[*] BUILDING LOCAL BINARY"
	@go build -o nomore403 main.go
	@$(HOME)/go/bin/nomore403 -h
