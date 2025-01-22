.PHONY: build

GOARCH := $(shell go env GOARCH)

build:
	env GOOS=linux GOARCH=$(GOARCH) go build -o bin/ocwrapper
