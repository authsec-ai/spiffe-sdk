# SPIFFE SDK for Go

A production-ready Go SDK for integrating SPIFFE/SPIRE authentication into your microservices with zero-trust security.

## Features

- **Automatic Owner Registration**: Register your service identity with the headless SPIRE service
- **Auto SVID Renewal**: Background process that automatically renews certificates before expiry
- **Incoming SVID Validation**: HTTP middleware to validate incoming certificates from other services
- **Outgoing SVID Attachment**: HTTP transport that automatically attaches your SVID to outbound calls
- **Hybrid Mode Support**: Works with both headless API and direct SPIRE workload API
- **Zero-Configuration mTLS**: Automatic mutual TLS setup for service-to-service communication

## Quick Start

### 1. Import the SDK

```go
import spiffesdk "path/to/spiffesdk"
```

### 2. Configure Your Service

```go
config := &spiffesdk.Config{
    // Service Identity
    ServiceName:     "my-service",
    SPIFFEID:        "spiffe://authsec.dev/my-service",
    ServiceType:     "application",

    // Kubernetes selectors
    Namespace:      "authsec",
    ServiceAccount: "authsec-sa",
    PodLabels: map[string]string{
        "app": "my-service",
    },

    // SPIRE Configuration
    HeadlessAPIURL: "https://dev.api.authsec.dev/spiresvc",
    SocketPath:     "/run/spire/sockets/agent.sock",
    TrustDomain:    "authsec.dev",

    // Auto-renewal settings
    RenewalThreshold: 5 * time.Minute,
    CheckInterval:    1 * time.Minute,
}
```

### 3. Initialize and Use

```go
// Create SDK instance
sdk, err := spiffesdk.NewSpiffeSDK(config)
if err != nil {
    log.Fatal("Failed to create SPIFFE SDK:", err)
}
defer sdk.Close()

// Initialize (register, attest, get SVID, start auto-renewal)
if err := sdk.Initialize(); err != nil {
    log.Fatal("Failed to initialize SPIFFE SDK:", err)
}

// Set up HTTP server with SPIFFE middleware
mux := http.NewServeMux()
mux.HandleFunc("/api/endpoint", myHandler)

// Add incoming SVID validation
protectedHandler := sdk.IncomingValidationMiddleware(mux)

// Start server with SPIFFE TLS
server := &http.Server{
    Addr:      ":8080",
    Handler:   protectedHandler,
    TLSConfig: sdk.GetHTTPClient().Transport.(*http.Transport).TLSClientConfig,
}

log.Fatal(server.ListenAndServeTLS("", ""))
```

## Integration Process

### Owner Registration Flow

1. **Service Startup**: Your service initializes the SPIFFE SDK
2. **Registration**: SDK calls headless API to register your workload
3. **Attestation**: SDK performs workload attestation
4. **SVID Issuance**: SDK receives initial SVID certificate
5. **Auto-Renewal**: Background goroutine monitors expiry and renews automatically

### Incoming Request Validation

1. **mTLS Handshake**: Client presents certificate during TLS connection
2. **Certificate Extraction**: Middleware extracts client certificate
3. **SPIFFE Validation**: SDK validates certificate against trust bundle
4. **Context Enrichment**: Caller's SPIFFE ID added to request context
5. **Business Logic**: Your handler receives authenticated request

### Outgoing Request Authentication

1. **HTTP Client**: Use `sdk.GetHTTPClient()` for outbound calls
2. **Automatic Attachment**: Transport automatically attaches your SVID
3. **mTLS Connection**: Establishes mutual TLS with target service
4. **Trust Validation**: Validates target service's certificate

## Configuration Options

### Service Identity

```go
type Config struct {
    ServiceName     string            // Human-readable service name
    SPIFFEID        string            // Full SPIFFE ID for this service
    ServiceType     string            // "application" or "system"

    // Kubernetes selectors for workload attestation
    Namespace       string
    ServiceAccount  string
    PodLabels       map[string]string
}
```

### SPIRE Integration

```go
type Config struct {
    HeadlessAPIURL  string        // URL of headless SPIRE service
    SocketPath      string        // Path to SPIRE agent socket
    TrustDomain     string        // SPIFFE trust domain
}
```

### Auto-Renewal Settings

```go
type Config struct {
    RenewalThreshold time.Duration  // Renew when TTL < threshold
    CheckInterval    time.Duration  // How often to check expiry
}
```

## Security Features

### Certificate Validation

- **Trust Bundle Verification**: All certificates validated against SPIRE trust bundle
- **Expiry Checking**: Automatic rejection of expired certificates
- **Revocation Support**: Integration with SPIRE revocation mechanisms
- **Format Normalization**: Automatic handling of various certificate formats

### Auto-Renewal

- **Proactive Renewal**: Certificates renewed before expiry
- **Failure Recovery**: Automatic retry on renewal failures
- **Graceful Degradation**: Continues operation if renewal temporarily fails
- **Background Operation**: No impact on request processing

## Advanced Usage

### Custom Validation Logic

```go
func (s *SpiffeSDK) CustomValidationMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        spiffeID := r.Context().Value("spiffe_id").(string)

        // Custom authorization logic
        if !isAuthorized(spiffeID, r.URL.Path) {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }

        next.ServeHTTP(w, r)
    })
}
```

### Manual SVID Operations

```go
// Get current SVID information
svid := sdk.GetCurrentSVID()

// Force SVID renewal
err := sdk.RefreshSVID()

// Validate specific certificate
result, err := sdk.ValidateIncomingSVID(certPEM)
```

## Deployment

### Kubernetes Requirements

1. **SPIRE Agent**: Must be running as DaemonSet with socket mounted
2. **Service Account**: Proper RBAC for workload attestation
3. **Network Policies**: Allow communication with headless SPIRE service

### Example Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-service
spec:
  template:
    spec:
      serviceAccountName: authsec-sa
      containers:
      - name: my-service
        image: my-service:latest
        volumeMounts:
        - name: spire-agent-socket
          mountPath: /run/spire/sockets
          readOnly: true
        env:
        - name: SPIFFE_ENDPOINT_SOCKET
          value: "unix:///run/spire/sockets/agent.sock"
      volumes:
      - name: spire-agent-socket
        hostPath:
          path: /run/spire/sockets
          type: Directory
```

## Error Handling

### Common Issues

1. **Socket Connection Failed**: Check SPIRE agent is running and socket is mounted
2. **Registration Failed**: Verify selectors match Kubernetes labels
3. **Certificate Validation Failed**: Check trust domain and certificate format
4. **Auto-Renewal Failed**: Monitor logs for SPIRE server connectivity

### Debugging

```go
// Enable debug logging
config.LogLevel = "debug"

// Check SVID status
svid := sdk.GetCurrentSVID()
log.Printf("SVID expires at: %v", svid.ExpiresAt)

// Test connectivity
err := sdk.TestConnection()
if err != nil {
    log.Printf("Connection test failed: %v", err)
}
```

## Examples

See the `examples/` directory for complete integration examples:

- `customer-service/`: Example customer service with SPIFFE integration
- `payment-service/`: Example payment service with cross-service calls
- `api-gateway/`: Example API gateway with service proxy

## Support

For issues and questions:
1. Check the logs for detailed error messages
2. Verify SPIRE agent connectivity
3. Validate Kubernetes selectors and labels
4. Review trust domain configuration

## License

[Your License Here]