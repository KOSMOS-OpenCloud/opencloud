SHELL := bash
NAME := thumbnails

include ../../.bingo/Variables.mk
include ../../.make/default.mk
include ../../.make/recursion.mk
include ../../.make/go.mk
include ../../.make/release.mk
include ../../.make/docs.mk
include ../../.make/protobuf.mk

.PHONY: go-generate
go-generate: protobuf

.PHONY: protobuf
protobuf: buf-generate
