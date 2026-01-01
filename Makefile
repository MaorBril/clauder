BINARY_NAME=clauder
BUILD_TAGS=fts5

.PHONY: build install clean test run format lint

build:
	go build -tags "$(BUILD_TAGS)" -o $(BINARY_NAME) .

install: build
	cp $(BINARY_NAME) ~/.bin/$(BINARY_NAME)

install-global: build
	sudo cp $(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)

clean:
	rm -f $(BINARY_NAME)
	go clean

test:
	go test -tags "$(BUILD_TAGS)" ./...

format:
	gofmt -w -s .
	go mod tidy

lint:
	go vet -tags "$(BUILD_TAGS)" ./...
	golangci-lint run --build-tags "$(BUILD_TAGS)"

run: build
	./$(BINARY_NAME)

serve: build
	./$(BINARY_NAME) serve

status: build
	./$(BINARY_NAME) status

# Development helpers
dev-remember: build
	./$(BINARY_NAME) remember "$(FACT)" -t "$(TAGS)"

dev-recall: build
	./$(BINARY_NAME) recall $(QUERY)

# Cross-compilation
build-linux:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -tags "$(BUILD_TAGS)" -o $(BINARY_NAME)-linux-amd64 .

build-all: build build-linux
