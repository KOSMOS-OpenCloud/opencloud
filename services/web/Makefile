SHELL := bash
NAME := web
WEB_ASSETS_VERSION = 552ff3c3f8e9543de653537087161c070698661e

include ../../.make/recursion.mk

############ tooling ############
ifneq (, $(shell command -v go 2> /dev/null)) # suppress `command not found warnings` for non go targets in CI
include ../../.bingo/Variables.mk
endif

############ go tooling ############
include ../../.make/go.mk

############ release #########
include ../../.make/release.mk

############ docs generate ############
include ../../.make/docs.mk

.PHONY: docs-generate
docs-generate: config-docs-generate

############ generate ############
include ../../.make/generate.mk

.PHONY: ci-go-generate
ci-go-generate: # CI runs ci-node-generate automatically before this target

.PHONY: ci-node-generate
ci-node-generate: pull-assets

.PHONY: pull-assets
pull-assets:
	git clean -xfd assets
	rm -rf _web && git clone -b main --depth 1 git@github.com:opencloud-eu/web.git _web
	make -C _web release
	tar xfv _web/release/web.tar.gz -C assets/core/
	cp -r _web/dev/docker/ocis-themes/opencloud assets/core/themes/ && rm -rf assets/core/themes/owncloud
	rm -rf _web

############ licenses ############
.PHONY: ci-node-check-licenses
ci-node-check-licenses: # ownCloud Web does the check on it's own

.PHONY: ci-node-save-licenses
ci-node-save-licenses:
	@mkdir -p ../../third-party-licenses/node/web
	@curl --fail -slL -o- https://github.com/owncloud/web/releases/download/$(WEB_ASSETS_VERSION)/third-party-licenses.tar.gz | tar xzf - -C ../../third-party-licenses/node/web
