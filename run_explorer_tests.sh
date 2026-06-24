#!/bin/bash

# Run explorer dispatch integration tests for specific models
# This script tests if LLMs will use the explorer agent for context search
#
# Usage:
#   ./run_explorer_tests.sh              # Run all models
#   ./run_explorer_tests.sh single       # Run one model at a time with visual feedback
#   OPENCODE_TEST_MODEL="opencode-go/deepseek-v4-flash" go test -v -run=TestExplorerDispatch_Integration ./internal/agent/

set -e

MODELS=(
    "opencode-go/deepseek-v4-flash"
    "opencode-go/mimo-v2.5"
    "opencode-go/minimax-m3"
)

echo "=== Explorer Dispatch Integration Tests ==="
echo "Testing if models use the explorer agent for context search"
echo ""

case "${1:-all}" in
  all)
    # Run all models in one go using the multi-model test
    MODELS_COMMA=$(IFS=,; echo "${MODELS[*]}")
    echo "Testing all models in sequence: $MODELS_COMMA"
    echo "-------------------------------------------"
    OPENCODE_TEST_MODELS="$MODELS_COMMA" go test -v -run=TestExplorerDispatch_MultipleModels ./internal/agent/ 2>&1
    ;;
  single)
    for model in "${MODELS[@]}"; do
      echo "Testing model: $model"
      echo "-------------------------------------------"

      if OPENCODE_TEST_MODEL="$model" go test -v -run=TestExplorerDispatch_Integration ./internal/agent/ 2>&1; then
        echo "✓ Model $model passed the test"
      else
        echo "✗ Model $model failed the test"
      fi

      echo ""
    done
    ;;
  with-tools)
    model="${2:-opencode-go/deepseek-v4-flash}"
    echo "Testing model $model with full tool context"
    echo "-------------------------------------------"
    OPENCODE_TEST_MODEL="$model" go test -v -run=TestExplorerDispatch_WithTools ./internal/agent/ 2>&1
    ;;
  *)
    echo "Usage: $0 [all|single|with-tools [model]]"
    echo ""
    echo "  all          Run all models in sequence (default)"
    echo "  single       Run each model individually"
    echo "  with-tools   Run with full tool context (read, glob, grep)"
    exit 1
    ;;
esac

echo ""
echo "=== Test Complete ==="