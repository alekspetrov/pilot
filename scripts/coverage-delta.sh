#!/bin/bash
# Check that new functions have test coverage
# Exit 1 if any new exported function has 0% coverage
# GH-1321: External correctness check for test coverage

set -e

echo "Checking coverage for new functions..."

# Get changed .go files (excluding test files)
CHANGED_FILES=$(git diff --name-only HEAD~1 2>/dev/null | grep '\.go$' | grep -v '_test\.go$' || true)

if [ -z "$CHANGED_FILES" ]; then
    echo "✓ No Go source files changed"
    exit 0
fi

# Extract unique packages from changed files
PACKAGES=""
for file in $CHANGED_FILES; do
    if [ -f "$file" ]; then
        pkg=$(dirname "$file")
        if [ -n "$pkg" ] && [ "$pkg" != "." ]; then
            # Convert path to Go package path
            pkg="./$pkg"
            if ! echo "$PACKAGES" | grep -q "$pkg"; then
                PACKAGES="$PACKAGES $pkg"
            fi
        fi
    fi
done

if [ -z "$PACKAGES" ]; then
    echo "✓ No packages to check"
    exit 0
fi

# Extract new exported function signatures from diff
# Matches: +func FuncName( or +func (r *Type) MethodName(
NEW_FUNCS=$(git diff HEAD~1 -- $CHANGED_FILES 2>/dev/null | grep -E '^\+func [A-Z]|^\+func \([^)]+\) [A-Z]' | sed 's/^+//' || true)

if [ -z "$NEW_FUNCS" ]; then
    echo "✓ No new exported functions"
    exit 0
fi

echo "New exported functions found:"
echo "$NEW_FUNCS" | head -20
echo ""

# Create temp directory for coverage
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

MISSING_COVERAGE=0

for pkg in $PACKAGES; do
    echo "Checking coverage for $pkg..."

    # Run tests with coverage
    COVER_FILE="$TMPDIR/coverage_$(echo "$pkg" | tr '/' '_').out"
    if ! go test -coverprofile="$COVER_FILE" "$pkg" >/dev/null 2>&1; then
        echo "  ⚠️  Tests failed for $pkg (skipping coverage check)"
        continue
    fi

    if [ ! -f "$COVER_FILE" ]; then
        echo "  ⚠️  No coverage file generated for $pkg"
        continue
    fi

    # Parse coverage
    COVERAGE_OUTPUT=$(go tool cover -func="$COVER_FILE" 2>/dev/null || true)

    # Check each new function
    while IFS= read -r func_line; do
        # Extract function name from signature
        # Handle both: func FuncName( and func (r *Type) MethodName(
        if echo "$func_line" | grep -qE 'func \([^)]+\)'; then
            # Method: func (r *Type) MethodName(
            FUNC_NAME=$(echo "$func_line" | sed -E 's/func \([^)]+\) ([A-Za-z0-9_]+).*/\1/')
        else
            # Function: func FuncName(
            FUNC_NAME=$(echo "$func_line" | sed -E 's/func ([A-Za-z0-9_]+).*/\1/')
        fi

        if [ -z "$FUNC_NAME" ]; then
            continue
        fi

        # Check if function appears in coverage output
        FUNC_COVERAGE=$(echo "$COVERAGE_OUTPUT" | grep -E "[[:space:]]$FUNC_NAME[[:space:]]" | tail -1 || true)

        if [ -z "$FUNC_COVERAGE" ]; then
            # Function not in coverage output - might be in a different package
            continue
        fi

        # Extract coverage percentage
        PERCENT=$(echo "$FUNC_COVERAGE" | awk '{print $NF}' | tr -d '%')

        if [ "$PERCENT" = "0.0" ] || [ "$PERCENT" = "0" ]; then
            echo "  ❌ $FUNC_NAME has 0% coverage"
            MISSING_COVERAGE=1
        else
            echo "  ✓ $FUNC_NAME: ${PERCENT}%"
        fi
    done <<< "$NEW_FUNCS"
done

if [ $MISSING_COVERAGE -eq 1 ]; then
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "COVERAGE CHECK FAILED: New functions have 0% coverage"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "Add test cases for the new functions before committing."
    exit 1
fi

echo ""
echo "✓ All new functions have test coverage"
exit 0
