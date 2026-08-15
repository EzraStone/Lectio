BINARY := lectio
PKG    := ./...

.PHONY: build test vet fmt tidy clean install corpus gate-a gate-a-corrected gate-a-symbols coupling

build:
	go build -o bin/$(BINARY) ./cmd/lectio

test:
	go test $(PKG)

vet:
	go vet $(PKG)

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

install:
	go install ./cmd/lectio

# Materialize the Gate A corpus. Tens of gigabytes and many minutes; the cache
# lives outside the working tree.
corpus: build
	./bin/$(BINARY) corpus fetch

# The go/no-go. --ablate costs nothing extra, and without it a FAIL says
# nothing you can act on.
gate-a: build
	./bin/$(BINARY) backtest --corpus corpus/gate-a.json --ablate -v

# Gate A scored against where newcomers went wrong rather than where they went.
# Reported beside the primary measure, never in place of it.
gate-a-corrected: build
	./bin/$(BINARY) backtest --corpus corpus/gate-a.json --ablate --target corrected -v

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
