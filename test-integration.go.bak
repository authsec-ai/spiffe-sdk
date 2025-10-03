package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	spiffesdk "spiffe-service/sdk" // Local import path
)

// Test Integration Script
// This demonstrates how the existing test pods would integrate with the SDK
func main() {
	fmt.Println("🧪 Testing SPIFFE SDK Integration")

	// Test 1: Customer Service Integration
	testCustomerServiceIntegration()

	// Test 2: Payment Service Integration
	testPaymentServiceIntegration()

	// Test 3: Cross-Service Communication
	testCrossServiceCommunication()

	fmt.Println("✅ All integration tests completed")
}

func testCustomerServiceIntegration() {
	fmt.Println("\n📋 Test 1: Customer Service Integration")

	config := &spiffesdk.Config{
		ServiceName:     "test-customer-service",
		SPIFFEID:        "spiffe://authsec.dev/test-customer-service",
		ServiceType:     "application",
		Namespace:       "authsec",
		ServiceAccount:  "authsec-sa",
		PodLabels: map[string]string{
			"app": "test-customer-service",
		},
		HeadlessAPIURL:   "http://localhost:7475", // Port-forwarded service
		SocketPath:       "/run/spire/sockets/agent.sock",
		TrustDomain:      "authsec.dev",
		RenewalThreshold: 5 * time.Minute,
		CheckInterval:    1 * time.Minute,
	}

	sdk, err := spiffesdk.NewSpiffeSDK(config)
	if err != nil {
		log.Printf("❌ Failed to create SDK for customer service: %v", err)
		return
	}
	defer sdk.Close()

	// Test registration and initial SVID
	if err := sdk.Initialize(); err != nil {
		log.Printf("❌ Failed to initialize customer service SDK: %v", err)
		return
	}

	fmt.Println("✅ Customer Service SDK initialized successfully")

	// Test getting current SVID
	currentSVID := sdk.GetCurrentSVID()
	if currentSVID != nil {
		fmt.Printf("✅ Current SVID expires at: %v\n", currentSVID.ExpiresAt)
	}
}

func testPaymentServiceIntegration() {
	fmt.Println("\n💳 Test 2: Payment Service Integration")

	config := &spiffesdk.Config{
		ServiceName:     "test-payment-service",
		SPIFFEID:        "spiffe://authsec.dev/test-payment-service",
		ServiceType:     "application",
		Namespace:       "authsec",
		ServiceAccount:  "authsec-sa",
		PodLabels: map[string]string{
			"app": "test-payment-service",
		},
		HeadlessAPIURL:   "http://localhost:7475",
		SocketPath:       "/run/spire/sockets/agent.sock",
		TrustDomain:      "authsec.dev",
		RenewalThreshold: 5 * time.Minute,
		CheckInterval:    1 * time.Minute,
	}

	sdk, err := spiffesdk.NewSpiffeSDK(config)
	if err != nil {
		log.Printf("❌ Failed to create SDK for payment service: %v", err)
		return
	}
	defer sdk.Close()

	if err := sdk.Initialize(); err != nil {
		log.Printf("❌ Failed to initialize payment service SDK: %v", err)
		return
	}

	fmt.Println("✅ Payment Service SDK initialized successfully")
}

func testCrossServiceCommunication() {
	fmt.Println("\n🔄 Test 3: Cross-Service Communication")

	// This would be how customer service calls payment service
	customerConfig := &spiffesdk.Config{
		ServiceName:      "test-customer-service",
		SPIFFEID:         "spiffe://authsec.dev/test-customer-service",
		ServiceType:      "application",
		Namespace:        "authsec",
		ServiceAccount:   "authsec-sa",
		PodLabels:        map[string]string{"app": "test-customer-service"},
		HeadlessAPIURL:   "http://localhost:7475",
		SocketPath:       "/run/spire/sockets/agent.sock",
		TrustDomain:      "authsec.dev",
		RenewalThreshold: 5 * time.Minute,
		CheckInterval:    1 * time.Minute,
	}

	customerSDK, err := spiffesdk.NewSpiffeSDK(customerConfig)
	if err != nil {
		log.Printf("❌ Failed to create customer SDK: %v", err)
		return
	}
	defer customerSDK.Close()

	if err := customerSDK.Initialize(); err != nil {
		log.Printf("❌ Failed to initialize customer SDK: %v", err)
		return
	}

	// Simulate making an authenticated call to payment service
	client := customerSDK.GetHTTPClient()
	if client != nil {
		fmt.Println("✅ HTTP client with SPIFFE mTLS created")
		fmt.Println("✅ Ready for cross-service communication")

		// In a real scenario, this would make an actual HTTP call:
		// resp, err := client.Get("https://test-payment-service.authsec.svc.cluster.local:8080/process")
		fmt.Println("🔐 mTLS-enabled client ready for secure service calls")
	}
}

// Example of how services would use the middleware
func demonstrateMiddleware() {
	fmt.Println("\n🛡️ Middleware Integration Example")

	config := &spiffesdk.Config{
		ServiceName:    "demo-service",
		SPIFFEID:       "spiffe://authsec.dev/demo-service",
		ServiceType:    "application",
		Namespace:      "authsec",
		ServiceAccount: "authsec-sa",
		PodLabels:      map[string]string{"app": "demo-service"},
		HeadlessAPIURL: "http://localhost:7475",
		SocketPath:     "/run/spire/sockets/agent.sock",
		TrustDomain:    "authsec.dev",
	}

	sdk, err := spiffesdk.NewSpiffeSDK(config)
	if err != nil {
		log.Printf("❌ Failed to create demo SDK: %v", err)
		return
	}
	defer sdk.Close()

	if err := sdk.Initialize(); err != nil {
		log.Printf("❌ Failed to initialize demo SDK: %v", err)
		return
	}

	// Set up HTTP handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/api/secure", func(w http.ResponseWriter, r *http.Request) {
		// Extract authenticated caller's SPIFFE ID
		spiffeID := r.Context().Value("spiffe_id")

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"message": "Secure endpoint accessed",
			"authenticated_caller": "%v",
			"timestamp": "%s"
		}`, spiffeID, time.Now().Format(time.RFC3339))
	})

	// Apply SPIFFE incoming validation middleware
	protectedHandler := sdk.IncomingValidationMiddleware(mux)

	fmt.Println("✅ HTTP server with SPIFFE middleware configured")
	fmt.Println("🔐 All incoming requests will be authenticated via SPIFFE certificates")

	// In production, you would start the server:
	// server := &http.Server{
	//     Addr:      ":8080",
	//     Handler:   protectedHandler,
	//     TLSConfig: sdk.GetHTTPClient().Transport.(*http.Transport).TLSClientConfig,
	// }
	// log.Fatal(server.ListenAndServeTLS("", ""))
}