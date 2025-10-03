package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	spiffesdk "path/to/spiffesdk" // Replace with actual import path
)

// Example: Payment Service Integration
func main() {
	// 1. Configure the SPIFFE SDK for Payment Service
	config := &spiffesdk.Config{
		// Service Identity
		ServiceName:     "payment-service",
		SPIFFEID:        "spiffe://authsec.dev/payment-service",
		ServiceType:     "application",

		// Kubernetes selectors
		Namespace:      "authsec",
		ServiceAccount: "authsec-sa",
		PodLabels: map[string]string{
			"app": "payment-service",
		},

		// SPIRE Configuration
		HeadlessAPIURL: "https://dev.api.authsec.dev/spiresvc",
		SocketPath:     "/run/spire/sockets/agent.sock",
		TrustDomain:    "authsec.dev",

		// Auto-renewal settings
		RenewalThreshold: 5 * time.Minute,
		CheckInterval:    1 * time.Minute,
	}

	// 2. Initialize SPIFFE SDK
	sdk, err := spiffesdk.NewSpiffeSDK(config)
	if err != nil {
		log.Fatal("Failed to create SPIFFE SDK:", err)
	}
	defer sdk.Close()

	// 3. Initialize the service
	if err := sdk.Initialize(); err != nil {
		log.Fatal("Failed to initialize SPIFFE SDK:", err)
	}

	fmt.Println("✅ Payment Service initialized with SPIFFE identity")

	// 4. Set up HTTP server with SPIFFE middleware
	mux := http.NewServeMux()

	// Add business logic handlers
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/process", processPaymentHandler(sdk))
	mux.HandleFunc("/validate", validatePaymentHandler(sdk))

	// 5. Wrap with SPIFFE incoming validation middleware
	protectedHandler := sdk.IncomingValidationMiddleware(mux)

	// 6. Start server
	server := &http.Server{
		Addr:      ":8080",
		Handler:   protectedHandler,
		TLSConfig: sdk.GetHTTPClient().Transport.(*http.Transport).TLSClientConfig,
	}

	fmt.Println("🚀 Payment Service starting on :8080 with SPIFFE mTLS")
	log.Fatal(server.ListenAndServeTLS("", ""))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status": "healthy", "service": "payment-service"}`)
}

func processPaymentHandler(sdk *spiffesdk.SpiffeSDK) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract caller's SPIFFE ID from context
		spiffeID := r.Context().Value("spiffe_id")

		// Validate that caller is authorized (e.g., customer-service)
		if spiffeID != "spiffe://authsec.dev/customer-service" {
			http.Error(w, "Unauthorized caller", http.StatusForbidden)
			return
		}

		// Process payment logic here
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"status": "success",
			"transaction_id": "txn_12345",
			"authenticated_caller": "%v",
			"timestamp": "%s"
		}`, spiffeID, time.Now().Format(time.RFC3339))
	}
}

func validatePaymentHandler(sdk *spiffesdk.SpiffeSDK) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Example of calling external service with SPIFFE mTLS
		client := sdk.GetHTTPClient()

		// Make call to user service for validation
		resp, err := client.Get("https://user-service.authsec.svc.cluster.local:8080/validate")
		if err != nil {
			http.Error(w, "Failed to validate with user service", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"validation": "completed", "method": "spiffe_mtls"}`)
	}
}