SHELL := bash
NAME := thumbnails

include ../../.bingo/Variables.mk
include ../../.make/default.mk
include ../../.make/deprecate.mk
include ../../.make/recursion.mk
include ../../.make/go.mk
include ../../.make/release.mk
include ../../.make/docs.mk
include ../../.make/protobuf.mk

.PHONY: go-generate-prod
go-generate-prod: protobuf

.PHONY: protobuf
protobuf: buf-generate
