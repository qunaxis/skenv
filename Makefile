# Development and release entry points. `make help` lists them.
GO            ?= go
STATICCHECK   ?= $(GO) run honnef.co/go/tools/cmd/staticcheck@2026.2.1
GOLANGCI_LINT ?= golangci-lint

.PHONY: help build test lint docs check check-commits hooks release snapshot demo clean

help: ## list targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

build: ## build ./skenv (version 0.0.0-dev+<sha>)
	$(GO) build -trimpath -o skenv ./cmd/skenv

test: ## go test -race
	$(GO) test -race ./...

lint: ## go mod tidy -diff, go vet, staticcheck, golangci-lint
	$(GO) mod tidy -diff
	$(GO) vet ./...
	$(STATICCHECK) ./...
	$(GOLANGCI_LINT) run ./...

docs: ## regenerate the command reference in docs/commands
	$(GO) run ./internal/tools/gendocs docs/commands

check: lint test check-commits ## everything CI runs

check-commits: ## every commit reachable from HEAD is a Conventional Commit
	scripts/check-commits.sh HEAD

hooks: ## install lefthook git hooks
	lefthook install

release: ## tag and publish the next version (needs GITHUB_TOKEN)
	scripts/release.sh

snapshot: ## local goreleaser build without publishing
	goreleaser release --snapshot --clean

demo: ## re-record docs/demo/demo.gif with vhs (sandbox in /tmp)
	vhs docs/demo/demo.tape

clean:
	rm -rf skenv dist
