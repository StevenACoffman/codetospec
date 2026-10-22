# The extractors are separate Go modules with their own dependency sets
# (AGENTS.md), so ./... from the root never reaches them: check and test
# loop over every module instead.
MODULES := . extractors/go extractors/scip extractors/webapp

GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK ?= govulncheck

.PHONY: build test run-fixture check lint race tidy vuln corpus

build:
	go build -o bin/codetospec ./cmd/codetospec

test:
	@set -e; for m in $(MODULES); do \
		echo "== test $$m"; (cd $$m && go vet ./... && go test ./...); \
	done

# Rebuilds first: a stale bin/ silently runs old code (AGENTS.md).
run-fixture: build
	bin/codetospec run --src testdata/fixture --out /tmp/spec-graph-fixture --facts testdata/fixture/fixture.facts.json

# check is the full gate CI runs: lint, tests, race detector, tidy go.mod
# files and known vulnerabilities, in every module.
check: lint test race tidy vuln

lint:
	@set -e; for m in $(MODULES); do \
		echo "== lint $$m"; (cd $$m && $(GOLANGCI_LINT) run --build-tags corpus ./...); \
	done

race:
	@set -e; for m in $(MODULES); do \
		echo "== race $$m"; (cd $$m && go test -race -count=1 ./...); \
	done

tidy:
	@set -e; for m in $(MODULES); do \
		echo "== tidy $$m"; (cd $$m && go mod tidy -diff); \
	done

vuln:
	@set -e; for m in $(MODULES); do \
		echo "== vuln $$m"; (cd $$m && $(GOVULNCHECK) ./...); \
	done

# corpus re-checks the rendered graphs of real repositories listed in
# internal/corpus/testdata/corpus.json (no LLM); entries not on this machine
# skip. CODETOSPEC_CORPUS_UPDATE=1 re-records the baselines.
corpus:
	go test -tags corpus -count=1 -v ./internal/corpus
