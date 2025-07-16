# Makefile for EMG Data Analysis Tool
# Testing Automation for Task 13

.PHONY: all test bench coverage lint clean install build help

# Default target for Task 13 testing automation
all: test bench coverage lint

# Help target
help:
	@echo "EMG Data Analysis Tool - Testing Automation (Task 13)"
	@echo ""
	@echo "Testing Automation Targets:"
	@echo "  test        - Run all tests"
	@echo "  test-unit   - Run unit tests only"
	@echo "  test-int    - Run integration tests only"
	@echo "  bench       - Run benchmark tests"
	@echo "  bench-std   - Run standard Go benchmarks"
	@echo "  coverage    - Run coverage analysis (90% target)"
	@echo "  lint        - Run linting with golangci-lint"
	@echo "  ci          - Run complete CI pipeline locally"
	@echo "  all         - Run test, bench, coverage, and lint"
	@echo ""
	@echo "Build Targets:"
	@echo "  build       - Build for current platform"
	@echo "  build-wails - Build Wails application"
	@echo "  build-all   - Build for all platforms"
	@echo ""
	@echo "Development Targets:"
	@echo "  install     - Install development dependencies"
	@echo "  clean       - Clean build artifacts and test files"
	@echo "  format      - Format code"
	@echo "  dev-setup   - Setup development environment"
	@echo ""

# ===================
# TESTING AUTOMATION (Task 13)
# ===================

# Test targets
test: test-unit test-int
	@echo "✓ All tests completed successfully!"

test-unit:
	@echo "Running unit tests..."
	go test -v ./internal/... ./gui/...

test-int:
	@echo "Running integration tests..."
	go test -v ./test/integration/...

test-race:
	@echo "Running race condition tests..."
	go test -race -short ./...

# Benchmark targets (Task 13.1)
bench:
	@echo "Running custom benchmarks..."
	go test -v ./test/benchmark/

bench-std:
	@echo "Running standard Go benchmarks..."
	go test -bench=. -benchmem -run=^$$ ./test/benchmark/

bench-all:
	@echo "Running all benchmarks..."
	go test -bench=. -benchmem ./test/benchmark/

# Coverage targets (Task 13.3)
coverage:
	@echo "Running coverage analysis with 90% target..."
	@chmod +x ./scripts/coverage.sh
	@sed -i '' 's/go test/~\/sdk\/go\/bin\/go test/g' ./scripts/coverage.sh
	@sed -i '' 's/go tool/~\/sdk\/go\/bin\/go tool/g' ./scripts/coverage.sh
	./scripts/coverage.sh

coverage-html:
	@echo "Generating HTML coverage report..."
	@mkdir -p coverage
	go test -coverprofile=coverage/coverage.out -covermode=atomic ./internal/... ./gui/...
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "Coverage report generated: coverage/coverage.html"

coverage-check:
	@echo "Checking 90% coverage threshold..."
	go test -coverprofile=coverage.out -covermode=atomic ./...
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	echo "Current coverage: $$COVERAGE%"; \
	if command -v bc >/dev/null 2>&1; then \
		if [ $$(echo "$$COVERAGE < 90" | bc -l) -eq 1 ]; then \
			echo "❌ Coverage $$COVERAGE% is below 90% threshold"; \
			exit 1; \
		else \
			echo "✅ Coverage $$COVERAGE% meets 90% threshold"; \
		fi; \
	else \
		if [ $$(echo "$$COVERAGE" | cut -d. -f1) -lt 90 ]; then \
			echo "❌ Coverage $$COVERAGE% is below 90% threshold"; \
			exit 1; \
		else \
			echo "✅ Coverage $$COVERAGE% meets 90% threshold"; \
		fi; \
	fi

# Linting targets
lint:
	@echo "Running golangci-lint..."
	golangci-lint run --timeout=5m

lint-fix:
	@echo "Running golangci-lint with auto-fix..."
	golangci-lint run --fix --timeout=5m

# CI targets (Task 13.2)
ci: test bench coverage lint security
	@echo "✅ Complete CI pipeline executed successfully!"

ci-fast: test-unit bench-std coverage-check lint
	@echo "✅ Fast CI pipeline executed successfully!"

# Security targets
security:
	@echo "Running security analysis..."
	gosec ./...

# ===================
# BUILD TARGETS
# ===================

# Build for current platform
build:
	go build -o emg_gui_tool main.go

# Build Wails application
build-wails:
	@echo "Building Wails application..."
	cd frontend && npm install && npm run build
	wails build

# Build for macOS
build-macos:
	go build -o emg_gui_tool_macos main.go

# Build for Windows using fyne-cross (requires Docker)
build-windows:
	@echo "Building for Windows requires fyne-cross and Docker"
	@echo "Install fyne-cross: go install github.com/fyne-io/fyne-cross@latest"
	@echo "Then run: fyne-cross windows -arch amd64 -output emg_gui_tool.exe ."

# Build for Linux using fyne-cross (requires Docker)
build-linux:
	@echo "Building for Linux requires fyne-cross and Docker"
	@echo "Install fyne-cross: go install github.com/fyne-io/fyne-cross@latest"
	@echo "Then run: fyne-cross linux -arch amd64 -output emg_gui_tool_linux ."

# Build all platforms using the build script
build-all:
	./build_cross.sh

# Build for multiple platforms
build-cross:
	@echo "Building for all platforms..."
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -o dist/count_mean_linux_amd64 .
	GOOS=linux GOARCH=arm64 go build -o dist/count_mean_linux_arm64 .
	GOOS=windows GOARCH=amd64 go build -o dist/count_mean_windows_amd64.exe .
	GOOS=windows GOARCH=arm64 go build -o dist/count_mean_windows_arm64.exe .
	GOOS=darwin GOARCH=amd64 go build -o dist/count_mean_darwin_amd64 .
	GOOS=darwin GOARCH=arm64 go build -o dist/count_mean_darwin_arm64 .

# ===================
# DEVELOPMENT TARGETS
# ===================

# Installation targets
install:
	@echo "Installing development dependencies..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
	go install github.com/wailsapp/wails/v2/cmd/wails@latest

install-tools:
	@echo "Installing additional tools..."
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
	go install github.com/client9/misspell/cmd/misspell@latest

# Development setup
dev-setup: install install-tools
	@echo "✅ Development environment setup completed!"

# Format targets
format:
	@echo "Formatting code..."
	gofmt -w .
	goimports -w .

check-format:
	@echo "Checking code format..."
	gofmt -l .
	goimports -l .

# Clean targets
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -rf dist/
	rm -rf build/
	rm -rf coverage/
	rm -f coverage.out
	rm -f *.prof
	rm -f *.out
	rm -f gosec.sarif
	rm -rf test_logs/
	rm -rf benchmark_logs/
	rm -rf benchmark_reports/
	rm -f emg_gui_tool*

clean-all: clean
	@echo "Cleaning all generated files..."
	rm -rf frontend/node_modules/
	rm -rf frontend/dist/
	go clean -cache
	go clean -modcache

# ===================
# COMPATIBILITY TARGETS
# ===================

# Old targets (kept for compatibility)
w-build:
	@echo "Direct cross-compilation not supported for GUI apps."
	@echo "Please use 'make build-windows' for instructions."

u-build:
	go build -o main main.go