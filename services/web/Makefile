SHELL := bash
NAME := web
WEB_ASSETS_VERSION = v1.0.0
WEB_ASSETS_BRANCH = main

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
ci-node-generate: download-assets

.PHONY: pull-assets
pull-assets:
	echo "using unreleased assets from branch $(WEB_ASSETS_BRANCH), this should not be used for official releases"
	git clean -xfd assets
	@if [ ! -d assets/core/origin ]; then \
		echo "no web assets found, initializing"; \
		git clone -b main --depth 1 https://github.com/opencloud-eu/web.git assets/core/origin; \
	else \
		echo "web assets found, updating"; \
		cd assets/core/origin; \
		if [[ `git status --porcelain` ]]; then echo "There are uncommitted changes in assets/core/origin"; exit 1; fi; \
		git checkout $(WEB_ASSETS_BRANCH); \
        git pull; \
	fi;
	make -C assets/core/origin release
	cd assets/core/origin && git checkout pnpm-lock.yaml # the lock could be updated during the release process, we need to reset the state for later runs
	tar xfv assets/core/origin/release/web.tar.gz -C assets/core/
	mkdir -p ../../third-party-licenses/node/web
	tar xfv assets/core/origin/release/third-party-licenses.tar.gz -C ../../third-party-licenses/node/web/

.PHONY: download-assets
download-assets:
	@if [ -d assets/core/origin ]; then rm -rf assets/core/origin; fi;
	git clean -xfd assets
	curl --fail -slL -o- https://github.com/opencloud-eu/web/releases/download/$(WEB_ASSETS_VERSION)/web.tar.gz | tar xzf - -C assets/core/

############ licenses ############
.PHONY: ci-node-check-licenses
ci-node-check-licenses: # OpenCloud Web does the check on it's own

.PHONY: ci-node-save-licenses
ci-node-save-licenses:
	@mkdir -p ../../third-party-licenses/node/web
	@curl --fail -slL -o- https://github.com/opencloud-eu/web/releases/download/$(WEB_ASSETS_VERSION)/third-party-licenses.tar.gz | tar xzf - -C ../../third-party-licenses/node/web
