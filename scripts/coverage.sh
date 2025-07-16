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

echo -e "${YELLOW}Running unit tests with coverage...${NC}"
~/sdk/go/bin/go test -coverprofile="$COVERAGE_FILE" -covermode=atomic ../internal/... ../gui/...

echo -e "${YELLOW}Running integration tests with coverage...${NC}"
~/sdk/go/bin/go test -coverprofile=integration_coverage.out -covermode=atomic ../test/integration/...

echo -e "${YELLOW}Running benchmark tests with coverage...${NC}"
~/sdk/go/bin/go test -coverprofile=benchmark_coverage.out -covermode=atomic ../test/benchmark/...

echo -e "${YELLOW}Merging coverage profiles...${NC}"
# Merge all coverage profiles
{
    echo "mode: atomic"
    tail -n +2 "$COVERAGE_FILE" 2>/dev/null || true
    tail -n +2 integration_coverage.out 2>/dev/null || true
    tail -n +2 benchmark_coverage.out 2>/dev/null || true
} > merged_coverage.out

# Use merged coverage as the main coverage file
mv merged_coverage.out "$COVERAGE_FILE"

echo -e "${YELLOW}Generating HTML coverage report...${NC}"
~/sdk/go/bin/go tool cover -html="$COVERAGE_FILE" -o "$COVERAGE_HTML"

echo -e "${YELLOW}Calculating coverage percentage...${NC}"
COVERAGE_PERCENT=$(~/sdk/go/bin/go tool cover -func="$COVERAGE_FILE" | grep total | awk '{print $3}' | sed 's/%//')

echo ""
echo -e "${BLUE}=== Coverage Report ===${NC}"
echo -e "Total Coverage: ${GREEN}${COVERAGE_PERCENT}%${NC}"
echo -e "Coverage File: ${PWD}/$COVERAGE_FILE"
echo -e "HTML Report: ${PWD}/$COVERAGE_HTML"
echo ""

# Detailed coverage by package
echo -e "${BLUE}=== Coverage by Package ===${NC}"
~/sdk/go/bin/go tool cover -func="$COVERAGE_FILE" | grep -v "total:" | sort -k3 -nr | head -20

echo ""
echo -e "${BLUE}=== Low Coverage Files (< 85%) ===${NC}"
~/sdk/go/bin/go tool cover -func="$COVERAGE_FILE" | grep -v "total:" | awk -F: '{
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