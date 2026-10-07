# Usage:
#   make                   build ./mediabrowser for this machine
#   make run DIR=~/Pictures  build and start serving DIR
#   make dist              build for Mac, Windows and Linux into dist/
#   make clean             remove build output
#   make vendor            update web/vendor from package.json (needs Node)

BINARY := mediabrowser
DIR    ?= .
PORT   ?= 8080
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build run dist clean vendor

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY) --port $(PORT) $(DIR)

dist:
	mkdir -p dist
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-mac-arm64 .
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-mac-intel .
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows.exe .
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 .
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 .

clean:
	rm -rf $(BINARY) dist

# The browser libraries in web/vendor are committed, so building needs only Go.
vendor:
	npm ci --ignore-scripts --no-audit --no-fund
	npm run --silent vendor
