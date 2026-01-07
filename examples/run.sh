#!/bin/bash

# Run example service with environment variables

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Audit Trail Example Service ===${NC}\n"

# Check if .env exists
if [ ! -f ".env" ]; then
    echo -e "${YELLOW}⚠️  .env file not found${NC}"
    echo -e "Creating .env from .env.example...\n"

    if [ -f ".env.example" ]; then
        cp .env.example .env
        echo -e "${GREEN}✅ .env created!${NC}"
        echo -e "${YELLOW}📝 Please edit .env with your actual credentials${NC}\n"
        echo "Press Enter to continue or Ctrl+C to exit and edit .env first..."
        read
    else
        echo -e "${RED}❌ .env.example not found${NC}"
        exit 1
    fi
fi

# Load .env
echo -e "${GREEN}📋 Loading environment variables...${NC}"
export $(cat .env | grep -v '^#' | xargs)

# Check required env vars
required_vars=("AUDIT_GCP_PROJECT" "AUDIT_PUBSUB_TOPIC" "AUDIT_PUBSUB_SUBSCRIPTION" "AUDIT_DB_DSN")
missing_vars=()

for var in "${required_vars[@]}"; do
    if [ -z "${!var}" ]; then
        missing_vars+=("$var")
    fi
done

if [ ${#missing_vars[@]} -ne 0 ]; then
    echo -e "${RED}❌ Missing required environment variables:${NC}"
    for var in "${missing_vars[@]}"; do
        echo -e "   - $var"
    done
    echo -e "\n${YELLOW}Please edit .env file and set these variables${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Environment variables loaded${NC}\n"

# Display config
echo -e "${GREEN}Configuration:${NC}"
echo "  GCP Project: ${AUDIT_GCP_PROJECT}"
echo "  Pub/Sub Topic: ${AUDIT_PUBSUB_TOPIC}"
echo "  Pub/Sub Subscription: ${AUDIT_PUBSUB_SUBSCRIPTION}"
echo "  Database Driver: ${AUDIT_DB_DRIVER:-pgx}"
echo "  Database Table: ${AUDIT_TABLE:-audit_trail}"
echo ""

# Check if service account key exists (if set)
if [ ! -z "$GOOGLE_APPLICATION_CREDENTIALS" ]; then
    if [ ! -f "$GOOGLE_APPLICATION_CREDENTIALS" ]; then
        echo -e "${RED}❌ Service account key file not found: $GOOGLE_APPLICATION_CREDENTIALS${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ Service account key found${NC}\n"
fi

# Run service
echo -e "${GREEN}🚀 Starting service on :8080...${NC}\n"
go run ex_service.go
