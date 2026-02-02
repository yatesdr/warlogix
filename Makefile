# Warlogix Makefile
# Build for multiple platforms

BINARY_NAME=warlogix
VERSION?=0.1.0
BUILD_DIR=build

# Build flags
LDFLAGS=-ldflags "-s -w -X main.Version=$(VERSION)"

.PHONY: all clean linux windows macos build run test

# Default target
all: clean linux windows macos

# Build for current platform
build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/warlogix

# Run the application
run:
	go run ./cmd/warlogix

# Run tests
test:
	go test -v ./...

# Linux builds
linux: linux-amd64 linux-arm64

linux-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/warlogix

linux-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/warlogix

# Windows builds
windows: windows-amd64

windows-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/warlogix

# macOS builds
macos: macos-amd64 macos-arm64

macos-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/warlogix

macos-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/warlogix

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY_NAME)

# Install to GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/warlogix

# Update dependencies
deps:
	go mod tidy
	go mod download

# Format code
fmt:
	go fmt ./...

# Lint code (requires golangci-lint)
lint:
	golangci-lint run

# Show help
help:
	@echo "Warlogix Build Targets:"
	@echo "  make build     - Build for current platform"
	@echo "  make run       - Run the application"
	@echo "  make all       - Build for all platforms"
	@echo "  make linux     - Build for Linux (amd64, arm64)"
	@echo "  make windows   - Build for Windows (amd64)"
	@echo "  make macos     - Build for macOS (amd64, arm64)"
	@echo "  make clean     - Remove build artifacts"
	@echo "  make install   - Install to GOPATH/bin"
	@echo "  make test      - Run tests"
	@echo "  make deps      - Update dependencies"
	@echo "  make fmt       - Format code"
