SHELL := bash
NAME := opencloud

TAGS := disable_crypt

ifdef ENABLE_VIPS
	TAGS := ${TAGS},enable_vips
endif

include ../.make/recursion.mk

############ tooling ############
ifneq (, $(shell command -v go 2> /dev/null)) # suppress `command not found warnings` for non go targets in CI
include ../.bingo/Variables.mk
GOARCH := $(shell go env GOARCH)
endif

############ go tooling ############
include ../.make/go.mk

############ release ############
include ../.make/release.mk

############ docs generate ############
CONFIG_DOCS_BASE_PATH := ../docs

include ../.make/docs.mk

.PHONY: docs-generate
docs-generate: config-docs-generate

############ dev-docker ############
.PHONY: dev-docker
dev-docker:
	$(MAKE) --no-print-directory release-linux-docker-$(GOARCH)
	docker build -f docker/Dockerfile.linux.$(GOARCH) -t opencloud-eu/opencloud:dev .

########### multiarch-docker ###########
.PHONY: dev-docker-multiarch
dev-docker-multiarch:
	@echo "+-------------------------------------------------------------------------+"
	@echo "| Are you sure you have run make ci-node-generate in the repository root? |"
	@echo "+-------------------------------------------------------------------------+"
	sleep 10
	docker buildx rm opencloudbuilder || true
	docker buildx create --platform linux/arm64,linux/amd64 --name opencloudbuilder
	docker buildx use opencloudbuilder
	cd .. && docker buildx build --platform linux/arm64,linux/amd64 --output type=docker --file opencloud/docker/Dockerfile.linux.multiarch --tag opencloud-eu/opencloud:dev-multiarch .
	docker buildx rm opencloudbuilder

############ debug-docker ############
.PHONY: debug-docker
debug-docker:
	$(MAKE) --no-print-directory debug-linux-docker-$(GOARCH)
	docker build -f docker/Dockerfile.linux.debug.$(GOARCH) -t opencloud-eu/opencloud:debug .

############ generate ############
include ../.make/generate.mk

.PHONY: ci-go-generate
ci-go-generate: # CI runs ci-node-generate automatically before this target

.PHONY: ci-node-generate
ci-node-generate: # opencloud needs assets of all other modules
	@if [ $(MAKE_DEPTH) -le 1 ]; then \
	$(MAKE) --no-print-directory -C .. ci-node-generate \
	; fi;

############ licenses ############
.PHONY: ci-node-check-licenses
ci-node-check-licenses:

.PHONY: ci-node-save-licenses
ci-node-save-licenses:
