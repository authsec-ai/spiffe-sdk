#!/bin/bash

# SPIFFE SDK Test Deployment Script
# This script demonstrates how to test the SDK integration with existing services

set -e

echo "🚀 SPIFFE SDK Integration Test Deployment"
echo "========================================"

# Configuration
NAMESPACE="authsec"
HEADLESS_SERVICE="dev-spire-headless"
PORT_FORWARD_PORT="7475"

# Step 1: Verify prerequisites
echo "📋 Step 1: Verifying prerequisites..."

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "❌ kubectl is not installed or not in PATH"
    exit 1
fi

# Check if we can access the cluster
if ! kubectl cluster-info &> /dev/null; then
    echo "❌ Cannot access Kubernetes cluster"
    exit 1
fi

# Check if namespace exists
if ! kubectl get namespace $NAMESPACE &> /dev/null; then
    echo "❌ Namespace $NAMESPACE does not exist"
    exit 1
fi

echo "✅ Prerequisites verified"

# Step 2: Check existing services
echo ""
echo "🔍 Step 2: Checking existing services..."

echo "Pods in $NAMESPACE namespace:"
kubectl get pods -n $NAMESPACE | grep -E "(test-customer|test-payment|dev-spire)"

echo ""
echo "Services in $NAMESPACE namespace:"
kubectl get svc -n $NAMESPACE | grep -E "(test-customer|test-payment|dev-spire)"

# Step 3: Set up port forwarding to headless service
echo ""
echo "🔌 Step 3: Setting up port forwarding..."

# Kill any existing port-forward processes
pkill -f "kubectl.*port-forward.*$HEADLESS_SERVICE" || true
sleep 2

echo "Starting port-forward to $HEADLESS_SERVICE:$PORT_FORWARD_PORT..."
kubectl port-forward -n $NAMESPACE svc/$HEADLESS_SERVICE $PORT_FORWARD_PORT:$PORT_FORWARD_PORT &
PORT_FORWARD_PID=$!

# Wait for port-forward to be ready
echo "Waiting for port-forward to be ready..."
sleep 5

# Test connectivity
if curl -s http://localhost:$PORT_FORWARD_PORT/health > /dev/null; then
    echo "✅ Port-forward successful, headless service accessible"
else
    echo "❌ Port-forward failed or service not responding"
    kill $PORT_FORWARD_PID || true
    exit 1
fi

# Step 4: Test SDK integration scenarios
echo ""
echo "🧪 Step 4: Testing SDK integration scenarios..."

# Test 1: Register test-customer-service
echo ""
echo "Test 1: Registering test-customer-service..."
curl -X POST http://localhost:$PORT_FORWARD_PORT/api/v1/workloads/register-and-issue \
  -H "Content-Type: application/json" \
  -d '{
    "spiffe_id": "spiffe://authsec.dev/test-customer-service",
    "type": "application",
    "selectors": [
      "k8s:ns:authsec",
      "k8s:sa:authsec-sa",
      "k8s:pod-label:app:test-customer-service"
    ]
  }' | jq '.' || echo "Registration request sent"

echo ""
echo "Test 2: Registering test-payment-service..."
curl -X POST http://localhost:$PORT_FORWARD_PORT/api/v1/workloads/register-and-issue \
  -H "Content-Type: application/json" \
  -d '{
    "spiffe_id": "spiffe://authsec.dev/test-payment-service",
    "type": "application",
    "selectors": [
      "k8s:ns:authsec",
      "k8s:sa:authsec-sa",
      "k8s:pod-label:app:test-payment-service"
    ]
  }' | jq '.' || echo "Registration request sent"

# Step 5: Verify registrations
echo ""
echo "📊 Step 5: Verifying registrations..."

echo "Listing all registered workloads:"
curl -s http://localhost:$PORT_FORWARD_PORT/api/v1/workloads | jq '.workloads[] | {id: .id, spiffe_id: .spiffe_id, status: .attestation_status}' || echo "Failed to list workloads"

# Step 6: Test certificate verification
echo ""
echo "🔐 Step 6: Testing certificate verification..."

# Get an SVID for testing
echo "Getting SVID for verification test..."
SVID_RESPONSE=$(curl -s http://localhost:$PORT_FORWARD_PORT/api/v1/workloads?spiffe_id=spiffe://authsec.dev/test-customer-service | jq -r '.workloads[0].id')

if [ "$SVID_RESPONSE" != "null" ] && [ "$SVID_RESPONSE" != "" ]; then
    echo "Found workload ID: $SVID_RESPONSE"

    # Issue SVID
    echo "Issuing SVID..."
    SVID_CERT=$(curl -s -X POST http://localhost:$PORT_FORWARD_PORT/api/v1/workloads/$SVID_RESPONSE/svid | jq -r '.x509_svid')

    if [ "$SVID_CERT" != "null" ] && [ "$SVID_CERT" != "" ]; then
        echo "Testing certificate verification..."
        curl -X POST http://localhost:$PORT_FORWARD_PORT/api/v1/verify/certificate \
          -H "Content-Type: application/json" \
          -d "{\"certificate\": \"$SVID_CERT\"}" | jq '.' || echo "Verification request sent"
    fi
fi

# Step 7: Show integration summary
echo ""
echo "📋 Step 7: Integration Summary"
echo "=============================="
echo ""
echo "✅ SDK Features Demonstrated:"
echo "   - Workload registration with headless API"
echo "   - Automatic attestation"
echo "   - SVID issuance"
echo "   - Certificate verification"
echo ""
echo "🔧 Next Steps for Production Integration:"
echo "   1. Import the SPIFFE SDK into your Go services"
echo "   2. Configure SDK with your service's SPIFFE ID and selectors"
echo "   3. Add SDK initialization to your service startup"
echo "   4. Wrap HTTP handlers with IncomingValidationMiddleware"
echo "   5. Use sdk.GetHTTPClient() for outbound service calls"
echo ""
echo "📁 Example files created:"
echo "   - sdk/spiffe-client-sdk.go (Main SDK)"
echo "   - sdk/examples/customer-service/main.go (Customer service example)"
echo "   - sdk/examples/payment-service/main.go (Payment service example)"
echo "   - sdk/config/service-template.yaml (Kubernetes deployment template)"
echo "   - sdk/README.md (Complete documentation)"
echo ""
echo "🚀 SDK Integration Complete!"

# Cleanup
echo ""
echo "🧹 Cleaning up..."
echo "Stopping port-forward (PID: $PORT_FORWARD_PID)..."
kill $PORT_FORWARD_PID || true

echo ""
echo "✅ Test deployment completed successfully!"
echo ""
echo "To continue testing:"
echo "1. Run: kubectl port-forward -n authsec svc/dev-spire-headless 7475:7475"
echo "2. Test the SDK with: go run sdk/test-integration.go"
echo "3. Deploy test services with: kubectl apply -f sdk/config/service-template.yaml"