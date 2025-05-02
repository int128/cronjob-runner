.PHONY: all
all:

.PHONY: test
test:
	go test -v -race ./internal/...

.PHONY: lint
lint:
	go tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint run --timeout 5m
