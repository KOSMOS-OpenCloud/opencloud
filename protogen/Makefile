SHELL := bash
NAME := protogen

include ../.bingo/Variables.mk
include ../.make/default.mk
include ../.make/recursion.mk
include ../.make/generate.mk

.PHONY: go-generate
go-generate: $(MOCKERY)
	$(MOCKERY)
