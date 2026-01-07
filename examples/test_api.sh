#!/bin/bash

# Test script untuk example service API

BASE_URL="http://localhost:8080"
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Testing Audit Trail Example Service ===${NC}\n"

# Function to print section
print_section() {
    echo -e "\n${BLUE}===== $1 =====${NC}"
}

# Function to test endpoint
test_endpoint() {
    local method=$1
    local path=$2
    local data=$3
    local headers=$4

    echo -e "${YELLOW}Request: $method $path${NC}"

    if [ -z "$data" ]; then
        if [ -z "$headers" ]; then
            curl -s -X $method "$BASE_URL$path" | jq '.'
        else
            curl -s -X $method "$BASE_URL$path" $headers | jq '.'
        fi
    else
        if [ -z "$headers" ]; then
            curl -s -X $method "$BASE_URL$path" \
                -H "Content-Type: application/json" \
                -d "$data" | jq '.'
        else
            curl -s -X $method "$BASE_URL$path" \
                -H "Content-Type: application/json" \
                $headers \
                -d "$data" | jq '.'
        fi
    fi
    echo ""
}

# Check if server is running
echo "Checking if server is running..."
if ! curl -s "$BASE_URL/health" > /dev/null 2>&1; then
    echo -e "${YELLOW}⚠️  Server is not running on $BASE_URL${NC}"
    echo "Please run: ./run.sh"
    exit 1
fi
echo -e "${GREEN}✅ Server is running${NC}\n"

# 1. Health Check (No Audit)
print_section "1. Health Check (No Audit)"
test_endpoint "GET" "/health"

# 2. Login (No Audit - Skipped)
print_section "2. Login (No Audit - Skipped)"
test_endpoint "POST" "/api/v1/login" '{"username":"admin","password":"secret"}'

TOKEN="Bearer valid-token-123"

# 3. List Products (With Audit)
print_section "3. List Products (With Audit)"
test_endpoint "GET" "/api/v1/products" "" "-H 'Authorization: $TOKEN'"

# 4. Get Single Product (With Audit)
print_section "4. Get Single Product (With Audit)"
test_endpoint "GET" "/api/v1/products/prod-1" "" "-H 'Authorization: $TOKEN'"

# 5. Create Product (With Audit + Body Capture)
print_section "5. Create Product (With Audit + Body Capture)"
test_endpoint "POST" "/api/v1/products" \
    '{"name":"Gaming Laptop","price":15000000,"stock":10}' \
    "-H 'Authorization: $TOKEN'"

# 6. Update Product (With Audit + Body Capture)
print_section "6. Update Product (With Audit + Body Capture)"
test_endpoint "PUT" "/api/v1/products/prod-1" \
    '{"name":"Gaming Laptop Pro","price":18000000,"stock":5}' \
    "-H 'Authorization: $TOKEN'"

# 7. Create Order (With Audit + Body Capture)
print_section "7. Create Order (With Audit + Body Capture)"
test_endpoint "POST" "/api/v1/orders" \
    '{"product_id":"prod-1","quantity":2}' \
    "-H 'Authorization: $TOKEN'"

# 8. Update Order Status (With Audit + Body Capture)
print_section "8. Update Order Status (With Audit + Body Capture)"
test_endpoint "PUT" "/api/v1/orders/order-123/status" \
    '{"status":"shipped"}' \
    "-H 'Authorization: $TOKEN'"

# 9. Cancel Order (With Audit + Body Capture)
print_section "9. Cancel Order (With Audit + Body Capture)"
test_endpoint "POST" "/api/v1/orders/order-123/cancel" \
    '{"reason":"Customer changed mind"}' \
    "-H 'Authorization: $TOKEN'"

# 10. Delete Product (With Audit)
print_section "10. Delete Product (With Audit)"
test_endpoint "DELETE" "/api/v1/products/prod-1" "" "-H 'Authorization: $TOKEN'"

# Summary
print_section "Summary"
echo -e "${GREEN}✅ All API tests completed!${NC}"
echo ""
echo -e "${YELLOW}📊 Check audit logs in database:${NC}"
echo "  SELECT * FROM audit_trail ORDER BY log_created_date DESC LIMIT 10;"
echo ""
echo -e "${YELLOW}🔍 Note:${NC}"
echo "  - /health dan /api/v1/login TIDAK ter-audit (skipped)"
echo "  - Semua endpoint lainnya TER-AUDIT dengan:"
echo "    • log_created_by: user-12345"
echo "    • log_request: request body (untuk POST/PUT/PATCH)"
echo "    • log_action: custom action name atau method + path"
