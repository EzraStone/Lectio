BINARY := lectio
PKG    := ./...

.PHONY: build test vet fmt fmt-check tidy clean install ci corpus corpus-holdout gate-a gate-a-corrected gate-a-symbols coupling holdout

build:
	go build -o bin/$(BINARY) ./cmd/lectio

test:
	go test $(PKG)

vet:
	go vet $(PKG)

fmt:
	gofmt -l -w .

# What CI checks, without rewriting anything. Run this before pushing rather
# than discovering the formatting job failed after the fact.
fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; echo "$$unformatted"; exit 1; \
	fi

# The same gates CI runs, in the same order.
ci: fmt-check vet test build

tidy:
	go mod tidy

install:
	go install ./cmd/lectio

# Materialize the Gate A corpus. Tens of gigabytes and many minutes; the cache
# lives outside the working tree.
corpus: build
	./bin/$(BINARY) corpus fetch

# The holdout corpus, disjoint from gate-a. Fetch it before running `holdout`.
corpus-holdout: build
	./bin/$(BINARY) corpus fetch --manifest corpus/gate-a-holdout.json

# The go/no-go. --ablate costs nothing extra, and without it a FAIL says
# nothing you can act on.
gate-a: build
	./bin/$(BINARY) backtest --corpus corpus/gate-a.json --ablate -v

# Gate A scored against where newcomers went wrong rather than where they went.
# Reported beside the primary measure, never in place of it.
gate-a-corrected: build
	./bin/$(BINARY) backtest --corpus corpus/gate-a.json --ablate --target corrected -v

# The confirmation run: the named candidate weightings, on repositories that
# did not produce them. This is the only comparison here where the hypothesis
# predates the evidence — everything else is measured on the corpus that
# generated it.
holdout: build
	./bin/$(BINARY) backtest --corpus corpus/gate-a-holdout.json --candidates --cases 12 -v

# The differentiator, measured directly. Precision@10 cannot see a signal that
# fires on a few dozen file pairs; this asks the claim as the spec states it.
coupling: build
	./bin/$(BINARY) backtest --coupling --corpus corpus/gate-a.json -v

clean:
	rm -rf bin/ backtest-out/

# Gate A graded in declarations rather than file paths, which removes the size
# prior every file-level measure carries.
gate-a-symbols: build
	./bin/$(BINARY) backtest --corpus corpus/gate-a.json --ablate --target symbols -v
