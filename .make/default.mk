.PHONY: node-generate-dev-default
node-generate-dev-default: node-generate-prod

.PHONY: node-generate-prod-default
node-generate-prod-default: noop

.PHONY: go-generate-default
go-generate-default: go-generate

.PHONY: generate
generate: generate-prod # production is always the default

.PHONY: generate-prod-default
generate-prod-default: node-generate-prod go-generate

.PHONY: generate-dev-default
generate-dev-default: node-generate-dev go-generate

.PHONY: vet
vet: noop

.PHONY: noop
noop:
	@echo -e "- $(MAKECMDGOALS): no action required\n"

.PHONY: %
%:  %-default
	@  true
