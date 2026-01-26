#!/bin/bash
# End-to-End Test Suite for Pilot
# Tests the full `pilot task` workflow
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Test fixture directories
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
FIXTURES_DIR="$PROJECT_ROOT/test/fixtures"
TEMP_DIR="/tmp/pilot-e2e-$$"

# Ensure pilot is built
PILOT_BIN="$PROJECT_ROOT/bin/pilot"

cleanup() {
    echo ""
    echo "Cleaning up..."
    rm -rf "$TEMP_DIR" 2>/dev/null || true
}

trap cleanup EXIT

log_test() {
    echo -e "${YELLOW}[TEST]${NC} $1"
    ((TESTS_RUN++))
}

log_pass() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((TESTS_PASSED++))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((TESTS_FAILED++))
}

log_info() {
    echo -e "[INFO] $1"
}

# Verify pilot binary exists
check_prerequisites() {
    log_info "Checking prerequisites..."

    if [ ! -f "$PILOT_BIN" ]; then
        echo "Pilot binary not found. Building..."
        (cd "$PROJECT_ROOT" && make build)
    fi

    if ! command -v claude &> /dev/null; then
        echo -e "${RED}Error: 'claude' command not found in PATH${NC}"
        echo "Claude Code CLI must be installed for E2E tests"
        exit 1
    fi

    log_info "Prerequisites OK"
}

# Create temp test directory
setup_temp_project() {
    local name="$1"
    local dir="$TEMP_DIR/$name"
    mkdir -p "$dir"
    cd "$dir"
    git init --quiet
    git config user.email "test@pilot.dev"
    git config user.name "Pilot E2E Test"
    echo "# Test Project" > README.md
    git add .
    git commit -m "Initial commit" --quiet
    echo "$dir"
}

# ============================================
# TEST CASES
# ============================================

test_dry_run_non_navigator() {
    log_test "Dry run mode (non-Navigator project)"

    local project_dir
    project_dir=$(setup_temp_project "dry-run-test")

    local output
    output=$("$PILOT_BIN" task "Create hello.py that prints Hello World" --project "$project_dir" --dry-run 2>&1)

    # Check output contains expected elements
    if echo "$output" | grep -q "DRY RUN" && \
       echo "$output" | grep -q "Constraints" && \
       echo "$output" | grep -q "ONLY create files explicitly mentioned"; then
        log_pass "Dry run shows improved constraints"
    else
        log_fail "Dry run output missing expected constraints"
        echo "Output was:"
        echo "$output"
        return 1
    fi
}

test_dry_run_navigator() {
    log_test "Dry run mode (Navigator project)"

    local project_dir
    project_dir=$(setup_temp_project "navigator-test")

    # Create Navigator structure
    mkdir -p "$project_dir/.agent"
    echo "# Navigator Project" > "$project_dir/.agent/DEVELOPMENT-README.md"

    local output
    output=$("$PILOT_BIN" task "Add a feature" --project "$project_dir" --dry-run 2>&1)

    # Check output contains Navigator-specific elements
    if echo "$output" | grep -q "Navigator" && \
       echo "$output" | grep -q "Start my Navigator session"; then
        log_pass "Dry run shows Navigator prompt"
    else
        log_fail "Dry run output missing Navigator elements"
        echo "Output was:"
        echo "$output"
        return 1
    fi
}

test_no_branch_flag() {
    log_test "--no-branch flag"

    local project_dir
    project_dir=$(setup_temp_project "no-branch-test")

    local output
    output=$("$PILOT_BIN" task "Test task" --project "$project_dir" --no-branch --dry-run 2>&1)

    if echo "$output" | grep -q "(current)" || \
       echo "$output" | grep -q "Work on current branch"; then
        log_pass "--no-branch flag works correctly"
    else
        log_fail "--no-branch flag not working"
        echo "Output was:"
        echo "$output"
        return 1
    fi
}

test_verbose_flag() {
    log_test "--verbose flag (dry-run, just checking flag handling)"

    local project_dir
    project_dir=$(setup_temp_project "verbose-test")

    # Just verify the flag is accepted
    if "$PILOT_BIN" task "Test" --project "$project_dir" --dry-run --verbose &>/dev/null; then
        log_pass "--verbose flag accepted"
    else
        log_fail "--verbose flag not accepted"
        return 1
    fi
}

test_prompt_constraints() {
    log_test "Non-Navigator prompt includes all constraints"

    local project_dir
    project_dir=$(setup_temp_project "constraints-test")

    local output
    output=$("$PILOT_BIN" task "Create hello.py" --project "$project_dir" --dry-run 2>&1)

    local all_constraints=true

    # Check for all required constraints
    if ! echo "$output" | grep -q "ONLY create files explicitly mentioned"; then
        echo "Missing: ONLY create files constraint"
        all_constraints=false
    fi
    if ! echo "$output" | grep -q "Do NOT create additional files"; then
        echo "Missing: Do NOT create additional files constraint"
        all_constraints=false
    fi
    if ! echo "$output" | grep -q "Do NOT modify existing files"; then
        echo "Missing: Do NOT modify existing files constraint"
        all_constraints=false
    fi
    if ! echo "$output" | grep -q "package.json"; then
        echo "Missing: package.json constraint"
        all_constraints=false
    fi

    if $all_constraints; then
        log_pass "All constraints present in prompt"
    else
        log_fail "Some constraints missing from prompt"
        return 1
    fi
}

test_task_id_generation() {
    log_test "Task ID generation"

    local project_dir
    project_dir=$(setup_temp_project "taskid-test")

    local output
    output=$("$PILOT_BIN" task "Test" --project "$project_dir" --dry-run 2>&1)

    if echo "$output" | grep -q "Task ID:.*TASK-"; then
        log_pass "Task ID generated correctly"
    else
        log_fail "Task ID not generated"
        echo "Output was:"
        echo "$output"
        return 1
    fi
}

# ============================================
# OPTIONAL: Live execution tests
# These actually run Claude Code - use with caution
# ============================================

test_live_simple_task() {
    if [ "$RUN_LIVE_TESTS" != "true" ]; then
        log_info "Skipping live test (set RUN_LIVE_TESTS=true to enable)"
        return 0
    fi

    log_test "Live: Simple file creation"

    local project_dir
    project_dir=$(setup_temp_project "live-simple-test")

    # Run actual task (this will invoke Claude Code)
    if timeout 120 "$PILOT_BIN" task "Create a file named hello.py with a single line: print('Hello World')" \
        --project "$project_dir" --no-branch 2>&1; then

        # Check if file was created
        if [ -f "$project_dir/hello.py" ]; then
            log_pass "File created successfully"
        else
            log_fail "File was not created"
            return 1
        fi
    else
        log_fail "Task execution failed or timed out"
        return 1
    fi
}

# ============================================
# MAIN
# ============================================

main() {
    echo "========================================"
    echo "Pilot E2E Test Suite"
    echo "========================================"
    echo ""

    check_prerequisites

    mkdir -p "$TEMP_DIR"

    echo ""
    echo "Running tests..."
    echo ""

    # Run all tests
    test_dry_run_non_navigator || true
    test_dry_run_navigator || true
    test_no_branch_flag || true
    test_verbose_flag || true
    test_prompt_constraints || true
    test_task_id_generation || true

    # Optional live tests
    test_live_simple_task || true

    echo ""
    echo "========================================"
    echo "Test Results"
    echo "========================================"
    echo "Total:  $TESTS_RUN"
    echo -e "Passed: ${GREEN}$TESTS_PASSED${NC}"
    echo -e "Failed: ${RED}$TESTS_FAILED${NC}"
    echo ""

    if [ $TESTS_FAILED -gt 0 ]; then
        echo -e "${RED}Some tests failed!${NC}"
        exit 1
    else
        echo -e "${GREEN}All tests passed!${NC}"
        exit 0
    fi
}

main "$@"
