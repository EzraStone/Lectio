BINARY := lectio
PKG    := ./...

.PHONY: build test vet fmt tidy clean install corpus gate-a

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

clean:
	rm -rf bin/ backtest-out/
