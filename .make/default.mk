.PHONY: node-generate-dev-default
node-generate-dev-default: node-generate-prod

.PHONY: node-generate-prod-default
node-generate-prod-default: noop

.PHONY: go-generate-dev-default
go-generate-dev-default: go-generate-prod

.PHONY: go-generate-prod-default
go-generate-prod-default: noop

.PHONY: generate
generate: generate-prod # production is always the default

.PHONY: generate-prod-default
generate-prod-default: node-generate-prod go-generate-prod

.PHONY: generate-dev-default
generate-dev-default: node-generate-dev go-generate-dev

.PHONY: vet
vet: noop

.PHONY: noop
noop:
	@echo -e "- $(MAKECMDGOALS): no action required\n"

.PHONY: %
%:  %-default
	@  true
