SHELL := bash
NAME := protogen

include ../.bingo/Variables.mk
include ../.make/default.mk
include ../.make/deprecate.mk
include ../.make/recursion.mk
include ../.make/generate.mk

.PHONY: go-generate-prod
go-generate-prod: $(MOCKERY)
	$(MOCKERY)
