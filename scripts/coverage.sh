#!/bin/bash

# Coverage script for EMG Data Analysis Tool
# This script runs comprehensive test coverage analysis

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
COVERAGE_DIR="coverage"
COVERAGE_FILE="coverage.out"
COVERAGE_HTML="coverage.html"
COVERAGE_THRESHOLD=90

echo -e "${BLUE}=== EMG Data Analysis Tool - Coverage Analysis ===${NC}"
echo -e "${BLUE}Target Coverage: ${COVERAGE_THRESHOLD}%${NC}"
echo ""

# Create coverage directory
mkdir -p "$COVERAGE_DIR"
cd "$COVERAGE_DIR"

# Clean previous coverage files
rm -f "$COVERAGE_FILE" "$COVERAGE_HTML"

echo -e "${YELLOW}Running all tests with unified coverage (-coverpkg)...${NC}"
# P1-H18:原本分三次跑(unit / integration / benchmark)再用
# `{ echo "mode: atomic"; tail -n +2 ...; }` 純文字 concat。這個合併方式
# 在 cover profile 規範下並不合法(同一 source 行可能在多個 profile 重複
# 出現,Go cover tool 對 atomic mode 預期已聚合過的計數,而非重複行),
# 會造成:
#   (a) `go tool cover -func` 報出超過 100% 或被低估的 coverage
#   (b) HTML 報表片段缺漏
# 改用單次 `-coverpkg` 統一蒐集:由 `./internal/... ./gui/...` 定義
# 「要被計入分母的程式碼集合」,而由 `./...` 提供「要跑的測試集合」
# (test/integration、test/benchmark、test/unit、test/phase_sync_test、
# internal/* 本身的同 package test 全都會跑)。Go cover tool 內建合法合併,
# 不需外部 gocovmerge,也避免 third-party 依賴。
#
# 排除 main package 與 frontend embed wrapper:`./...` 含 root,但 cover
# 統計只看 -coverpkg 列表,所以 main.go 不會混入。
go test \
    -coverprofile="$COVERAGE_FILE" \
    -covermode=atomic \
    -coverpkg=count_mean/internal/...,count_mean/gui/... \
    ../...

echo -e "${YELLOW}Generating HTML coverage report...${NC}"
go tool cover -html="$COVERAGE_FILE" -o "$COVERAGE_HTML"

echo -e "${YELLOW}Calculating coverage percentage...${NC}"
COVERAGE_PERCENT=$(go tool cover -func="$COVERAGE_FILE" | grep total | awk '{print $3}' | sed 's/%//')

echo ""
echo -e "${BLUE}=== Coverage Report ===${NC}"
echo -e "Total Coverage: ${GREEN}${COVERAGE_PERCENT}%${NC}"
echo -e "Coverage File: ${PWD}/$COVERAGE_FILE"
echo -e "HTML Report: ${PWD}/$COVERAGE_HTML"
echo ""

# Detailed coverage by package
echo -e "${BLUE}=== Coverage by Package ===${NC}"
go tool cover -func="$COVERAGE_FILE" | grep -v "total:" | sort -k3 -nr | head -20

echo ""
echo -e "${BLUE}=== Low Coverage Files (< 85%) ===${NC}"
go tool cover -func="$COVERAGE_FILE" | grep -v "total:" | awk -F: '{
    split($2, parts, " ");
    coverage = parts[length(parts)];
    gsub(/%/, "", coverage);
    if (coverage < 85) {
        printf "%-50s %s\n", $1, coverage"%";
    }
}' | sort -k2 -n

echo ""

# Check if coverage meets threshold
if (( $(echo "$COVERAGE_PERCENT >= $COVERAGE_THRESHOLD" | bc -l) )); then
    echo -e "${GREEN}✓ Coverage ${COVERAGE_PERCENT}% meets ${COVERAGE_THRESHOLD}% threshold${NC}"
    exit 0
else
    echo -e "${RED}✗ Coverage ${COVERAGE_PERCENT}% is below ${COVERAGE_THRESHOLD}% threshold${NC}"
    
    # Show suggestions for improvement
    echo ""
    echo -e "${YELLOW}Suggestions for improvement:${NC}"
    echo "1. Add more unit tests for uncovered functions"
    echo "2. Add integration tests for complex workflows"
    echo "3. Add edge case testing"
    echo "4. Review and test error handling paths"
    echo ""
    
    exit 1
fi