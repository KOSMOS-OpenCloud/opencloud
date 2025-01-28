
SKIP_CONFIG_DOCS_GENERATE ?= 0
CONFIG_DOCS_BASE_PATH ?= ../../docs/services

.PHONY: grpc-docs-generate
grpc-docs-generate: buf-generate
