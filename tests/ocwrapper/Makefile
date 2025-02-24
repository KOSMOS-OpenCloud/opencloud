GOOS := linux ## only linux is supported
GOARCH := $(shell go env GOARCH)

.PHONY: build
build:
	GOWORK=off \
	GOOS=$(GOOS) \
	GOARCH=$(GOARCH) \
	go build -o bin/ocwrapper
