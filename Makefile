BINARY := lectio
PKG    := ./...

.PHONY: build test vet fmt tidy clean install

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

clean:
	rm -rf bin/ backtest-out/
