SHELL := bash
NAME := collaboration

include ../../.bingo/Variables.mk
include ../../.make/default.mk
include ../../.make/recursion.mk
include ../../.make/go.mk
include ../../.make/release.mk
include ../../.make/docs.mk

.PHONY: go-generate
go-generate: $(MOCKERY)
	$(MOCKERY)
