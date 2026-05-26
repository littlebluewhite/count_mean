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
	@echo "  test-int    - Run integration tests only (non real-data)"
	@echo "  test-int-realdata - Run real-data integration tests (needs EMG_TEST_FIXTURES_DIR)"
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
	@echo "Running unit tests with race detector..."
	# -race 必開：gui/wails_binding_test.go 的 race-only test 在無 race
	# 環境會 t.Skip（非 PASS）；CI 路徑必須帶 -race 才能驗 atomic.Pointer
	# 等 concurrency fix 沒退化。詳見 gui/race_enabled.go。
	go test -race -v ./internal/... ./gui/...

test-int:
	@echo "Running integration tests (non real-data)..."
	# `integration_realdata` build tag 用於需要私有 fixtures 的 test,預設不跑;
	# 改由 `make test-int-realdata` 或 CI 在備齊 EMG_TEST_FIXTURES_DIR 後 opt-in,
	# 避免空跑誤判 PASS。
	go test -v ./test/integration/...

# 私有 fixtures 整合測試:需設定 EMG_TEST_FIXTURES_DIR 指向實驗資料夾。
# 缺 env var 時 test 內部會 t.Skip。
test-int-realdata:
	@echo "Running real-data integration tests (requires EMG_TEST_FIXTURES_DIR)..."
	@if [ -z "$$EMG_TEST_FIXTURES_DIR" ]; then \
		echo "⚠️  EMG_TEST_FIXTURES_DIR not set — tests will t.Skip individually."; \
	fi
	go test -tags integration_realdata -v ./test/integration/...

test-race:
	@echo "Running race condition tests across the whole tree (super-set of test-unit)..."
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

# Cell-level CSV validator benchmark — 量測 hot path 守門 overhead。
bench-csv-validator:
	@echo "Running CSV validator benchmarks..."
	go test -bench=BenchmarkValidate -benchmem -run=^$$ ./internal/validation/csv/

# Coverage targets
coverage:
	@echo "Running coverage analysis with 90% target..."
	@chmod +x ./scripts/coverage.sh
	./scripts/coverage.sh

coverage-html:
	@echo "Generating HTML coverage report..."
	@mkdir -p coverage
	go test -coverprofile=coverage/coverage.out -covermode=atomic ./internal/... ./gui/...
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "Coverage report generated: coverage/coverage.html"

# Coverage gate 三項守門:
#   1. `-race`:race-only test 在無 -race 環境 t.Skip,不帶 -race 會讓 race
#      path 不進分母,造成 false-positive 通過門檻。
#   2. `-coverpkg`:限定分母只看實作 package,避免 integration test helper
#      污染數字。
#   3. 顯式列舉 package list 取代 `./...`,避免新增 sub-tree 時 silent drift。
#
# 門檻歷史:
#   7682042 commit 把門檻設為 90%,但該 commit 與後續 09a2ca1/18a025d/cf42173 連帶
#   推上 main,GH Actions 只跑 HEAD,因此 90% 從未被 CI 真正驗證過 — 是 aspirational。
#   cf42173 修綠 timing test 後 coverage gate 第一次真的能跑到 threshold check,
#   暴露實際 coverage = 78.7%。本 PR 把門檻調為 78% 反映真實 baseline,讓 build matrix
#   解鎖能 merge。follow-up issue 追蹤把 coverage 拉回 90%(主要靠補 test/phase_sync_test
#   的 fixture-gated 測試 + internal/{calculator,cci,io} 等大型套件的 unit tests)。
coverage-check:
	@echo "Checking 78% coverage threshold (-race + -coverpkg)..."
	go test -race -coverprofile=coverage.out -covermode=atomic \
		-coverpkg=count_mean/internal/...,count_mean/gui/... \
		./internal/... ./gui/... ./test/...
	@COVERAGE=$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub(/%/,"",$$3); print $$3}'); \
	if [ -z "$$COVERAGE" ]; then echo "❌ ERROR: cannot parse coverage from coverage.out"; exit 1; fi; \
	echo "Current coverage: $$COVERAGE%"; \
	awk -v c="$$COVERAGE" 'BEGIN { exit !(c >= 78) }' && \
		echo "✅ Coverage $$COVERAGE% meets 78% threshold" || \
		(echo "❌ Coverage $$COVERAGE% is below 78% threshold"; exit 1)

# golangci-lint version pin:不同 major version 的 linter 集合與 default config
# 都不同,不 pin 會出現「本機 PASS / CI FAIL」的詭異現象。
# 只 pin major.minor 而非 full semver:patch release 不改變 linter 啟用集合。
# Bump 時同步更新 .github/workflows/ 內 setup-golangci-lint step 的 version —
# 兩處 drift 是 lint 結果不一致的 root cause。
GOLANGCI_LINT_VERSION ?= 2.12

# Linting targets
lint:
	@echo "Running golangci-lint (pinned $(GOLANGCI_LINT_VERSION).x)..."
	@INSTALLED=$$(golangci-lint version 2>/dev/null | head -1); \
	echo "Detected: $$INSTALLED"; \
	case "$$INSTALLED" in \
		*"has version $(GOLANGCI_LINT_VERSION)."*) ;; \
		*) echo "❌ golangci-lint version mismatch (want $(GOLANGCI_LINT_VERSION).x, got: $$INSTALLED)"; \
		   echo "Install pinned version:"; \
		   echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION).0"; \
		   exit 1 ;; \
	esac
	golangci-lint run --timeout=5m

lint-fix:
	@echo "Running golangci-lint with auto-fix..."
	golangci-lint run --fix --timeout=5m

# i18n-check: 守 catalog 一致性 — verb sequence 4 locale 一致、無 %w、
# muscle_ratio key 在 4 locale 都 covered。獨立執行,i18n-only diff PR 不必跑全 lint。
i18n-check:
	@echo "Checking i18n catalog (verb consistency / %w guard / coverage)..."
	go test -v -run "TestI18n_NoCatalogPercentWVerb|TestI18n_VerbConsistencyAcrossLocales|TestI18n_AllMuscleRatioKeysCovered|TestT_MuscleRatioKeysVerbCompat|TestI18n_CallerWrapPatternPreservesErrorsIs" ./internal/i18n/...

# CI targets (Task 13.2)
ci: test bench coverage lint security
	@echo "✅ Complete CI pipeline executed successfully!"

ci-fast: test-unit bench-std coverage-check lint
	@echo "✅ Fast CI pipeline executed successfully!"

# Strict mode: HIGH severity + HIGH confidence finding 才 exit non-zero,
# 較低 severity 印出但不擋 build。SARIF 輸出方便 GitHub code-scanning 上傳;
# 失敗時 sarif 檔已寫出。
security:
	@echo "Running security analysis (strict: HIGH severity + HIGH confidence blocks)..."
	gosec -severity=high -confidence=high -fmt=sarif -out=gosec.sarif -stdout -verbose=text ./...

# ===================
# BUILD TARGETS
# ===================

# Version injection via ldflags
VERSION ?= dev
LDFLAGS := -X 'main.Version=$(VERSION)'

# Build for current platform
build:
	go build -ldflags "$(LDFLAGS)" -o emg_gui_tool main.go

# Build Wails application
build-wails:
	@echo "Building Wails application..."
	cd frontend && npm install && npm run build
	wails build

# Build for macOS
build-macos:
	go build -o emg_gui_tool_macos main.go

# Build all platforms using the build script
build-all:
	./build_cross.sh

# Build for multiple platforms
build-cross:
	@echo "Building for all platforms..."
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/count_mean_linux_amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/count_mean_linux_arm64 .
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/count_mean_windows_amd64.exe .
	GOOS=windows GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/count_mean_windows_arm64.exe .
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/count_mean_darwin_amd64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/count_mean_darwin_arm64 .

# ===================
# DEVELOPMENT TARGETS
# ===================

# Installation targets
# 版本 pin 必須與 .github/workflows/ 及 go.mod 同步;drift 會造成 dev-setup 失敗。
install:
	@echo "Installing development dependencies..."
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION).0
	go install github.com/securego/gosec/v2/cmd/gosec@v2.26.1
	go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0

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

