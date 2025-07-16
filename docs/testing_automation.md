# Testing Automation Documentation (Task 13)

## Overview

This document describes the testing automation implementation for the EMG Data Analysis Tool, completed as part of Task 13. The automation includes standardized benchmarks, CI/CD pipeline, and comprehensive coverage reporting.

## Table of Contents

1. [Testing Framework](#testing-framework)
2. [Benchmark Tests](#benchmark-tests)
3. [CI/CD Pipeline](#ci-cd-pipeline)
4. [Coverage Reporting](#coverage-reporting)
5. [Makefile Commands](#makefile-commands)
6. [Development Workflow](#development-workflow)
7. [Troubleshooting](#troubleshooting)

## Testing Framework

### Structure

```
test/
├── benchmark/
│   ├── benchmark_test.go           # Custom benchmark tests
│   ├── standard_benchmark_test.go  # Standard Go benchmarks
│   └── benchmark_demo.go           # Demo and examples
├── unit/
│   ├── calculator/                 # Calculator module tests
│   ├── chart/                      # Chart module tests
│   ├── config/                     # Configuration tests
│   └── [other modules]/            # Other unit tests
└── integration/
    ├── integration_test.go         # Integration tests
    └── [other integration tests]/  # Additional integration tests
```

### Test Categories

- **Unit Tests**: Test individual components in isolation
- **Integration Tests**: Test component interactions
- **Benchmark Tests**: Performance and memory usage tests
- **End-to-End Tests**: Complete workflow testing

## Benchmark Tests

### Task 13.1: Standard Go Benchmarks

The project now includes both custom benchmarks and standard Go benchmarks:

#### Custom Benchmarks (Legacy)
```go
// Custom benchmark approach
benchmarker := benchmark.NewBenchmarker(cfg)
metrics := benchmarker.Benchmark("Test Name", func() error {
    // Test logic
    return nil
})
```

#### Standard Go Benchmarks (New)
```go
// Standard Go benchmark format
func BenchmarkSomething(b *testing.B) {
    for i := 0; i < b.N; i++ {
        // Test logic
    }
}
```

### Available Benchmarks

1. **BenchmarkMathCalculation** - Mathematical operations
2. **BenchmarkStringProcessing** - String manipulation
3. **BenchmarkArrayProcessing** - Array operations
4. **BenchmarkCSVReading** - CSV file reading
5. **BenchmarkCSVParsing** - CSV data parsing
6. **BenchmarkMaxMeanCalculation** - Statistical calculations
7. **BenchmarkDataNormalization** - Data normalization
8. **BenchmarkPhaseAnalysis** - Phase analysis
9. **BenchmarkConcurrentDataProcessing** - Concurrent processing
10. **BenchmarkMemoryIntensiveOperation** - Memory usage tests
11. **BenchmarkLargeFileProcessing** - Large file handling

### Running Benchmarks

```bash
# Run all benchmarks
make bench

# Run standard Go benchmarks
make bench-std

# Run benchmarks with memory profiling
go test -bench=. -benchmem ./test/benchmark/

# Run specific benchmark
go test -bench=BenchmarkMathCalculation ./test/benchmark/
```

## CI/CD Pipeline

### Task 13.2: GitHub Actions Integration

The CI/CD pipeline is defined in `.github/workflows/ci.yml` and includes:

#### Jobs

1. **Test Job**
   - Runs on multiple Go versions (1.21-1.24)
   - Executes unit and integration tests
   - Includes race condition detection

2. **Benchmark Job**
   - Runs performance benchmarks
   - Tracks performance regressions
   - Generates benchmark reports

3. **Coverage Job**
   - Measures test coverage
   - Enforces 90% coverage threshold
   - Uploads reports to Codecov

4. **Lint Job**
   - Runs golangci-lint
   - Enforces code quality standards
   - Includes security analysis

5. **Security Job**
   - Runs Gosec security scanner
   - Uploads SARIF reports
   - Detects security vulnerabilities

6. **Build Job**
   - Builds for multiple platforms
   - Creates distributable artifacts
   - Includes Wails application build

7. **Deploy Job**
   - Deploys on main branch
   - Creates GitHub releases
   - Publishes artifacts

#### Trigger Conditions

- Push to `main` or `develop` branches
- Pull requests to `main` or `develop`
- Manual workflow dispatch

#### Environment Variables

- `GO_VERSION`: Target Go version (1.24)
- `GITHUB_TOKEN`: For GitHub API access
- `CODECOV_TOKEN`: For coverage reporting

## Coverage Reporting

### Task 13.3: 90% Coverage Target

The project enforces a 90% test coverage threshold:

#### Configuration

- **Target**: 90% overall coverage
- **Threshold**: 1% deviation allowed
- **Scope**: All packages except test files

#### Coverage Tools

1. **Codecov Integration**
   - Automatic coverage reporting
   - Pull request comments
   - Coverage trends

2. **Coverage Script**
   - `scripts/coverage.sh` - Comprehensive coverage analysis
   - Generates HTML reports
   - Identifies low-coverage areas

3. **Makefile Targets**
   - `make coverage` - Full coverage analysis
   - `make coverage-html` - HTML report generation
   - `make coverage-check` - Threshold validation

#### Coverage Reports

```bash
# Generate coverage report
make coverage

# Check coverage threshold
make coverage-check

# Generate HTML report
make coverage-html
```

#### Coverage Exclusions

- Test files (`*_test.go`)
- Generated files (`*.pb.go`, `*.gen.go`)
- Frontend assets
- Build artifacts
- Main entry point

## Makefile Commands

### Testing Commands

```bash
# Run all tests
make test

# Run unit tests only
make test-unit

# Run integration tests only
make test-int

# Run race condition tests
make test-race
```

### Benchmark Commands

```bash
# Run custom benchmarks
make bench

# Run standard benchmarks
make bench-std

# Run all benchmarks
make bench-all
```

### Coverage Commands

```bash
# Full coverage analysis
make coverage

# HTML coverage report
make coverage-html

# Coverage threshold check
make coverage-check
```

### CI/CD Commands

```bash
# Complete CI pipeline
make ci

# Fast CI pipeline
make ci-fast

# Security analysis
make security

# Linting
make lint

# Format code
make format
```

### Development Commands

```bash
# Setup development environment
make dev-setup

# Install dependencies
make install

# Clean artifacts
make clean

# Help
make help
```

## Development Workflow

### Local Development

1. **Setup Environment**
   ```bash
   make dev-setup
   ```

2. **Run Tests**
   ```bash
   make test
   ```

3. **Check Coverage**
   ```bash
   make coverage
   ```

4. **Run Linting**
   ```bash
   make lint
   ```

5. **Format Code**
   ```bash
   make format
   ```

### Pre-commit Workflow

1. Run unit tests: `make test-unit`
2. Check code format: `make check-format`
3. Run linting: `make lint`
4. Check coverage: `make coverage-check`

### Pull Request Workflow

1. All CI jobs must pass
2. Coverage must meet 90% threshold
3. No security vulnerabilities
4. Code must be properly formatted

## Troubleshooting

### Common Issues

1. **Coverage Below Threshold**
   - Add more unit tests
   - Test error handling paths
   - Review uncovered functions

2. **Benchmark Failures**
   - Check system resources
   - Verify test data availability
   - Review timeout settings

3. **CI/CD Failures**
   - Check GitHub Actions logs
   - Verify dependency versions
   - Review environment variables

4. **Linting Errors**
   - Run `make lint-fix` for auto-fixes
   - Review `.golangci.yml` configuration
   - Check code formatting

### Debug Commands

```bash
# Verbose test output
go test -v ./...

# Race condition detection
go test -race ./...

# Memory profiling
go test -bench=. -memprofile=mem.prof ./test/benchmark/

# CPU profiling
go test -bench=. -cpuprofile=cpu.prof ./test/benchmark/

# Coverage with detailed output
go test -cover -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

### Performance Monitoring

```bash
# Performance benchmarks
make bench-all

# Memory usage analysis
go test -bench=. -benchmem ./test/benchmark/

# Performance regression detection
go test -bench=. -benchmem -count=5 ./test/benchmark/
```

## Configuration Files

### `.golangci.yml`
- Linting rules and configuration
- Enabled/disabled linters
- Custom settings per linter

### `.codecov.yml`
- Coverage reporting configuration
- Threshold settings
- Ignore patterns

### `.github/workflows/ci.yml`
- CI/CD pipeline definition
- Job configurations
- Trigger conditions

## Best Practices

1. **Write Tests First**: Follow TDD approach
2. **Maintain High Coverage**: Aim for 90%+ coverage
3. **Use Benchmarks**: Monitor performance regularly
4. **Run CI Locally**: Use `make ci` before pushing
5. **Format Code**: Use `make format` consistently
6. **Review Security**: Monitor security reports

## Integration with IDE

### VS Code
- Install Go extension
- Configure test runner
- Set up coverage display

### GoLand
- Enable test coverage
- Configure benchmark runner
- Set up linting integration

## Metrics and Reporting

### Coverage Metrics
- Overall coverage percentage
- Package-level coverage
- Function-level coverage
- Line coverage details

### Benchmark Metrics
- Execution time
- Memory allocation
- Operations per second
- Throughput measurements

### CI/CD Metrics
- Build success rate
- Test pass rate
- Coverage trends
- Performance trends

## Future Enhancements

1. **Performance Monitoring**
   - Continuous benchmarking
   - Performance regression alerts
   - Resource usage tracking

2. **Test Automation**
   - Automated test generation
   - Property-based testing
   - Fuzzing integration

3. **Quality Gates**
   - Code quality metrics
   - Technical debt monitoring
   - Security scanning

## Conclusion

The testing automation implementation successfully addresses all requirements of Task 13:

- ✅ **13.1**: Standard Go benchmarks implemented
- ✅ **13.2**: GitHub Actions CI/CD pipeline configured
- ✅ **13.3**: 90% coverage target enforced

The system provides comprehensive testing, continuous integration, and quality assurance for the EMG Data Analysis Tool.