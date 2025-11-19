#!/bin/bash

# Quick test script for API services

set -e

BASE_URL="${1:-http://localhost}"

echo "🧪 Testing API Services"
echo "======================"
echo "Base URL: $BASE_URL"
echo ""

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

test_endpoint() {
    local name=$1
    local url=$2
    local expected_status=${3:-200}
    
    echo -n "Testing $name... "
    
    status=$(curl -s -o /dev/null -w "%{http_code}" "$url")
    
    if [ "$status" -eq "$expected_status" ]; then
        echo -e "${GREEN}✓ OK${NC} (HTTP $status)"
        return 0
    else
        echo -e "${RED}✗ FAILED${NC} (Expected HTTP $expected_status, got $status)"
        return 1
    fi
}

# Health Checks
echo "📊 Health Checks"
test_endpoint "Aggregate Health" "$BASE_URL/health"
test_endpoint "Core Service" "$BASE_URL/v1/core/health"
test_endpoint "Media Service" "$BASE_URL/v1/media/health"
test_endpoint "Hasura" "$BASE_URL/v1/links/healthz"

echo ""
echo "📄 API Documentation"
test_endpoint "Core OpenAPI" "$BASE_URL/v1/core/openapi.json"
test_endpoint "Media OpenAPI" "$BASE_URL/v1/media/openapi.json"

echo ""
echo "🎉 All tests passed!"
