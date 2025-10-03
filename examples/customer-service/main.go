package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	spiffesdk "path/to/spiffesdk" // Replace with actual import path
)

// Example: Customer Service Integration
func main() {
	// 1. Configure the SPIFFE SDK
	config := &spiffesdk.Config{
		// Service Identity
		ServiceName:     "customer-service",
		SPIFFEID:        "spiffe://authsec.dev/customer-service",
		ServiceType:     "application",

		// Kubernetes selectors
		Namespace:      "authsec",
		ServiceAccount: "authsec-sa",
		PodLabels: map[string]string{
			"app": "customer-service",
		},

		// SPIRE Configuration
		HeadlessAPIURL: "https://dev.api.authsec.dev/spiresvc",
		SocketPath:     "/run/spire/sockets/agent.sock",
		TrustDomain:    "authsec.dev",

		// Auto-renewal settings
		RenewalThreshold: 5 * time.Minute,  // Renew when TTL < 5 minutes
		CheckInterval:    1 * time.Minute,  // Check every minute
	}

	// 2. Initialize SPIFFE SDK
	sdk, err := spiffesdk.NewSpiffeSDK(config)
	if err != nil {
		log.Fatal("Failed to create SPIFFE SDK:", err)
	}
	defer sdk.Close()

	// 3. Initialize the service (register with headless API, get initial SVID, start auto-renewal)
	if err := sdk.Initialize(); err != nil {
		log.Fatal("Failed to initialize SPIFFE SDK:", err)
	}

	fmt.Println("✅ Customer Service initialized with SPIFFE identity")

	// 4. Set up HTTP server with SPIFFE middleware
	mux := http.NewServeMux()

	// Add business logic handlers
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/customer/", customerHandler)
	mux.HandleFunc("/internal/payment", paymentServiceHandler(sdk))

	// 5. Wrap with SPIFFE incoming validation middleware
	protectedHandler := sdk.IncomingValidationMiddleware(mux)

	// 6. Start HTTPS server with SPIFFE TLS
	server := &http.Server{
		Addr:      ":8080",
		Handler:   protectedHandler,
		TLSConfig: sdk.GetHTTPClient().Transport.(*http.Transport).TLSClientConfig,
	}

	fmt.Println("🚀 Customer Service starting on :8080 with SPIFFE mTLS")
	log.Fatal(server.ListenAndServeTLS("", "")) // Certificates come from SPIFFE
}

// Business logic handlers
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status": "healthy", "service": "customer-service"}`)
}

func customerHandler(w http.ResponseWriter, r *http.Request) {
	// Extract SPIFFE ID from context (set by incoming validation middleware)
	spiffeID := r.Context().Value("spiffe_id")

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{
		"message": "Customer data",
		"authenticated_caller": "%v",
		"customer_id": "12345"
	}`, spiffeID)
}

// Example of making outgoing calls to other services with SPIFFE mTLS
func paymentServiceHandler(sdk *spiffesdk.SpiffeSDK) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Create HTTP client with SPIFFE mTLS for outgoing calls
		client := sdk.GetHTTPClient()

		// Make authenticated call to payment service
		resp, err := client.Get("https://payment-service.authsec.svc.cluster.local:8080/process")
		if err != nil {
			http.Error(w, "Failed to call payment service", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"message": "Payment processed via SPIFFE mTLS"}`)
	}
}