SHELL := /bin/bash

##@ Tests

.PHONY: lint
lint: lint-yaml lint-actionlint lint-zizmor lint-python ## Run all linters

.PHONY: lint-yaml
# renovate: datasource=docker depName=cytopia/yamllint
YAMLLINT_VERSION = 1-0.10
lint-yaml: ## Lint YAML files
	@if command -v yamllint &> /dev/null; then \
		yamllint --strict --config-file .yamllint.yml .; \
	else \
		docker run --rm -v $(shell pwd):/data cytopia/yamllint:$(YAMLLINT_VERSION) --strict --config-file .yamllint.yml .; \
	fi

.PHONY: lint-actionlint
# renovate: datasource=docker depName=rhysd/actionlint
ACTIONLINT_VERSION = 1.7.12
lint-actionlint: ## Lint GitHub Actions workflows
	@if command -v actionlint &> /dev/null; then \
		actionlint; \
	else \
		docker run --rm -v $(shell pwd):/src --workdir /src rhysd/actionlint:$(ACTIONLINT_VERSION); \
	fi

.PHONY: lint-zizmor
# renovate: datasource=docker depName=ghcr.io/zizmorcore/zizmor
ZIZMOR_VERSION = 1.27.0
lint-zizmor: ## Statically analyze GitHub Actions workflows
	@if command -v zizmor &> /dev/null; then \
		zizmor .; \
	else \
		docker run --rm -v $(shell pwd):/src --workdir /src ghcr.io/zizmorcore/zizmor:$(ZIZMOR_VERSION) .; \
	fi

.PHONY: lint-python
# renovate: datasource=docker depName=ghcr.io/astral-sh/ruff
RUFF_VERSION = 0.14.3
PYTHON_SCRIPTS = $(shell find . -type f -name "*.py" -not -path "./.git/*")
lint-python: ## Lint Python scripts
	@if command -v ruff &> /dev/null; then \
		ruff check $(PYTHON_SCRIPTS); \
	else \
		docker run --rm -v $(shell pwd):/src --workdir /src ghcr.io/astral-sh/ruff:$(RUFF_VERSION) check $(PYTHON_SCRIPTS); \
	fi

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
