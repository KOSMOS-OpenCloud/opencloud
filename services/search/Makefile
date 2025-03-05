SHELL := bash
NAME := search

include ../../.bingo/Variables.mk
include ../../.make/default.mk
include ../../.make/deprecate.mk
include ../../.make/recursion.mk
include ../../.make/go.mk
include ../../.make/release.mk
include ../../.make/docs.mk

.PHONY: go-generate-prod
go-generate-prod: $(PIGEON) $(MOCKERY)
	$(MOCKERY)
	$(PIGEON) -optimize-grammar -optimize-parser -o ../../pkg/kql/dictionary_gen.go ../../pkg/kql/dictionary.peg
