BINARY := bin/herdr-prompt-library

.PHONY: build
build:
	@mkdir -p $(dir $(BINARY))
	go build -o $(BINARY) ./cmd/herdr-prompt-library
