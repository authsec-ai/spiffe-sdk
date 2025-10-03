package spiffesdk

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// SpiffeSDK provides complete SPIFFE integration for microservices
type SpiffeSDK struct {
	config       *Config
	headlessAPI  *HeadlessAPI
	workloadAPI  *workloadapi.X509Source
	currentSVID  *SVIDCache
	httpClient   *http.Client
	tlsConfig    *tls.Config
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
}

// Config holds SDK configuration
type Config struct {
	// Service Identity
	ServiceName     string `json:"service_name"`
	SPIFFEID        string `json:"spiffe_id"`
	ServiceType     string `json:"service_type"`     // "application" or "system"

	// Kubernetes selectors
	Namespace       string `json:"namespace"`
	ServiceAccount  string `json:"service_account"`
	PodLabels       map[string]string `json:"pod_labels"`

	// SPIRE Configuration
	HeadlessAPIURL  string `json:"headless_api_url"`
	SocketPath      string `json:"socket_path"`
	TrustDomain     string `json:"trust_domain"`

	// Auto-renewal settings
	RenewalThreshold time.Duration `json:"renewal_threshold"` // Renew when TTL < threshold
	CheckInterval    time.Duration `json:"check_interval"`    // How often to check expiry
}

// SVIDCache holds current SVID and metadata
type SVIDCache struct {
	SVID       string    `json:"svid"`
	PrivateKey string    `json:"private_key"`
	Bundle     string    `json:"bundle"`
	ExpiresAt  time.Time `json:"expires_at"`
	IssuedAt   time.Time `json:"issued_at"`
	mu         sync.RWMutex
}

// HeadlessAPI client for headless SPIRE service
type HeadlessAPI struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewSpiffeSDK creates a new SPIFFE SDK instance
func NewSpiffeSDK(config *Config) (*SpiffeSDK, error) {
	ctx, cancel := context.WithCancel(context.Background())

	sdk := &SpiffeSDK{
		config: config,
		headlessAPI: &HeadlessAPI{
			BaseURL: config.HeadlessAPIURL,
			HTTPClient: &http.Client{
				Timeout: 10 * time.Second, // Add timeout to prevent hanging
			},
		},
		currentSVID: &SVIDCache{},
		ctx:         ctx,
		cancel:      cancel,
	}

	// Initialize workload API for direct SPIRE integration (optional - may not be available yet)
	// If it fails, we'll try again during Initialize() after registration
	_ = sdk.initWorkloadAPI()

	return sdk, nil
}

// Initialize performs the complete setup process
func (s *SpiffeSDK) Initialize() error {
	// Step 1: Register with headless API (owner registration)
	if err := s.registerWithHeadlessAPI(); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Step 1.5: Try to initialize workload API now (after registration)
	if s.workloadAPI == nil {
		_ = s.initWorkloadAPI() // Ignore error, will use headless API for SVIDs
	}

	// Step 2: Get initial SVID
	if err := s.refreshSVID(); err != nil {
		return fmt.Errorf("initial SVID fetch failed: %w", err)
	}

	// Step 3: Setup TLS configuration (only if workload API is available)
	if s.workloadAPI != nil {
		if err := s.setupTLSConfig(); err != nil {
			return fmt.Errorf("TLS setup failed: %w", err)
		}
	}

	// Step 4: Start auto-renewal background process
	go s.startAutoRenewal()

	return nil
}

// Register service with headless SPIRE API
func (s *SpiffeSDK) registerWithHeadlessAPI() error {
	selectors := []string{
		fmt.Sprintf("k8s:ns:%s", s.config.Namespace),
		fmt.Sprintf("k8s:sa:%s", s.config.ServiceAccount),
	}

	// Add pod labels as selectors
	for key, value := range s.config.PodLabels {
		selectors = append(selectors, fmt.Sprintf("k8s:pod-label:%s:%s", key, value))
	}

	payload := map[string]interface{}{
		"spiffe_id": s.config.SPIFFEID,
		"type":      s.config.ServiceType,
		"selectors": selectors,
	}

	return s.headlessAPI.RegisterAndIssueSVID(payload)
}

// Refresh SVID from headless API
func (s *SpiffeSDK) refreshSVID() error {
	svid, err := s.headlessAPI.GetOrRefreshSVID(s.config.SPIFFEID)
	if err != nil {
		return err
	}

	s.currentSVID.mu.Lock()
	s.currentSVID.SVID = svid.X509SVID
	s.currentSVID.PrivateKey = svid.PrivateKey
	s.currentSVID.Bundle = svid.Bundle
	s.currentSVID.ExpiresAt = svid.ExpiresAt
	s.currentSVID.IssuedAt = svid.IssuedAt
	s.currentSVID.mu.Unlock()

	return nil
}

// Auto-renewal background process
func (s *SpiffeSDK) startAutoRenewal() {
	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.currentSVID.mu.RLock()
			timeToExpiry := time.Until(s.currentSVID.ExpiresAt)
			s.currentSVID.mu.RUnlock()

			if timeToExpiry <= s.config.RenewalThreshold {
				if err := s.refreshSVID(); err != nil {
					// Log error but continue trying
					fmt.Printf("SVID renewal failed: %v\n", err)
				} else {
					fmt.Printf("SVID renewed successfully, expires at: %v\n", s.currentSVID.ExpiresAt)
				}
			}
		}
	}
}

// GetCurrentSVID returns the current SVID certificate (base64 encoded)
func (s *SpiffeSDK) GetCurrentSVID() string {
	s.currentSVID.mu.RLock()
	defer s.currentSVID.mu.RUnlock()
	return s.currentSVID.SVID
}

// GetHTTPClient returns an HTTP client configured with SPIFFE mTLS for internal service calls
// Use this for calling other services in the same trust domain
func (s *SpiffeSDK) GetHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: s.tlsConfig,
		},
		Timeout: 30 * time.Second,
	}
}

// NewInternalHTTPClient creates an HTTP client that automatically uses mTLS for internal services
// and regular HTTP for external services
func (s *SpiffeSDK) NewInternalHTTPClient(internalDomains []string) *http.Client {
	return &http.Client{
		Transport: &smartTransport{
			sdk:             s,
			internalDomains: internalDomains,
			mtlsTransport: &http.Transport{
				TLSClientConfig: s.tlsConfig,
			},
			regularTransport: http.DefaultTransport,
		},
		Timeout: 30 * time.Second,
	}
}

// smartTransport switches between mTLS and regular HTTP based on target
type smartTransport struct {
	sdk              *SpiffeSDK
	internalDomains  []string
	mtlsTransport    http.RoundTripper
	regularTransport http.RoundTripper
}

func (t *smartTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Check if this is an internal service call
	host := req.URL.Host
	if host == "" {
		host = req.Host
	}

	for _, domain := range t.internalDomains {
		// Check for exact match or suffix match (e.g., .svc.cluster.local)
		if host == domain || (len(domain) > 0 && domain[0] == '.' && hasSuffix(host, domain)) {
			// Use mTLS for internal services
			return t.mtlsTransport.RoundTrip(req)
		}
		// Check if host contains the domain (for k8s services like service.namespace.svc.cluster.local)
		if len(domain) > 1 && domain[0] != '.' && (host == domain || hasSuffix(host, "."+domain)) {
			return t.mtlsTransport.RoundTrip(req)
		}
	}
	// Use regular HTTP for external services
	return t.regularTransport.RoundTrip(req)
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// GetHTTPServer returns an HTTP server configured with SPIFFE mTLS and validation middleware
func (s *SpiffeSDK) GetHTTPServer(addr string, handler http.Handler, validateIncoming bool) *http.Server {
	var finalHandler http.Handler
	if validateIncoming {
		// Wrap with validation middleware
		finalHandler = s.IncomingValidationMiddleware(handler)
	} else {
		finalHandler = handler
	}

	return &http.Server{
		Addr:      addr,
		Handler:   finalHandler,
		TLSConfig: tlsconfig.MTLSServerConfig(s.workloadAPI, s.workloadAPI, tlsconfig.AuthorizeAny()),
	}
}

// ValidateIncomingSVID validates an incoming certificate
func (s *SpiffeSDK) ValidateIncomingSVID(cert string) (*ValidationResult, error) {
	payload := map[string]string{
		"certificate": cert,
	}

	return s.headlessAPI.VerifyCertificate(payload)
}

// IncomingValidationMiddleware for HTTP servers
func (s *SpiffeSDK) IncomingValidationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract client certificate from TLS connection
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			clientCert := r.TLS.PeerCertificates[0]

			// Convert to PEM format for validation
			certPEM := s.certToPEM(clientCert)

			result, err := s.ValidateIncomingSVID(certPEM)
			if err != nil || !result.Valid {
				http.Error(w, "Invalid client certificate", http.StatusUnauthorized)
				return
			}

			// Add SPIFFE ID to request context
			ctx := context.WithValue(r.Context(), "spiffe_id", result.SPIFFEID)
			r = r.WithContext(ctx)
		}

		next.ServeHTTP(w, r)
	})
}

// OutgoingAttachmentMiddleware for HTTP clients
func (s *SpiffeSDK) OutgoingAttachmentMiddleware(rt http.RoundTripper) http.RoundTripper {
	return &spiffeMTLSTransport{
		sdk:       s,
		transport: rt,
	}
}

// spiffeMTLSTransport implements http.RoundTripper with SPIFFE mTLS
type spiffeMTLSTransport struct {
	sdk       *SpiffeSDK
	transport http.RoundTripper
}

func (t *spiffeMTLSTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request and add SPIFFE TLS config
	req = req.Clone(req.Context())

	// Use the SPIFFE-configured transport
	if t.transport == nil {
		t.transport = &http.Transport{
			TLSClientConfig: t.sdk.tlsConfig,
		}
	}

	return t.transport.RoundTrip(req)
}

// Helper functions and types...

type ValidationResult struct {
	Valid     bool   `json:"valid"`
	SPIFFEID  string `json:"spiffe_id"`
	Subject   string `json:"subject"`
	Issuer    string `json:"issuer"`
	NotBefore string `json:"not_before"`
	NotAfter  string `json:"not_after"`
}

type SVIDResponse struct {
	ID         string    `json:"id"`
	WorkloadID string    `json:"workload_id"`
	SPIFFEID   string    `json:"spiffe_id"`
	X509SVID   string    `json:"x509_svid"`
	PrivateKey string    `json:"private_key"`
	Bundle     string    `json:"bundle"`
	ExpiresAt  time.Time `json:"expires_at"`
	IssuedAt   time.Time `json:"issued_at"`
}

// Implementation of helper methods...
func (s *SpiffeSDK) initWorkloadAPI() error {
	// Create a timeout context for workload API initialization
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	source, err := workloadapi.NewX509Source(
		ctx,
		workloadapi.WithClientOptions(
			workloadapi.WithAddr("unix://"+s.config.SocketPath),
		),
	)
	if err != nil {
		return err
	}
	s.workloadAPI = source
	return nil
}

func (s *SpiffeSDK) setupTLSConfig() error {
	// Create SPIFFE-aware TLS config
	s.tlsConfig = tlsconfig.MTLSClientConfig(s.workloadAPI, s.workloadAPI, tlsconfig.AuthorizeAny())
	return nil
}

func (s *SpiffeSDK) certToPEM(cert *x509.Certificate) string {
	// Convert x509.Certificate to PEM format
	certPEM := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	return string(pem.EncodeToMemory(certPEM))
}

// HeadlessAPI methods...
func (api *HeadlessAPI) RegisterAndIssueSVID(payload map[string]interface{}) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", api.BaseURL+"/spiresvc/api/v1/workloads/register-and-issue", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := api.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("registration failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (api *HeadlessAPI) GetOrRefreshSVID(spiffeID string) (*SVIDResponse, error) {
	// Try to get existing SVID first by listing workloads
	req, err := http.NewRequest("GET", api.BaseURL+"/spiresvc/api/v1/workloads?spiffe_id="+spiffeID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := api.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get workload with status %d: %s", resp.StatusCode, string(body))
	}

	var workloadResp struct {
		Workloads []struct {
			ID       string `json:"id"`
			SPIFFEID string `json:"spiffe_id"`
		} `json:"workloads"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&workloadResp); err != nil {
		return nil, fmt.Errorf("failed to decode workload response: %w", err)
	}

	if len(workloadResp.Workloads) == 0 {
		return nil, fmt.Errorf("workload not found for SPIFFE ID: %s", spiffeID)
	}

	workloadID := workloadResp.Workloads[0].ID

	// Issue new SVID
	svidReq, err := http.NewRequest("POST", api.BaseURL+"/spiresvc/api/v1/workloads/"+workloadID+"/svid", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create SVID request: %w", err)
	}

	svidResp, err := api.HTTPClient.Do(svidReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send SVID request: %w", err)
	}
	defer svidResp.Body.Close()

	if svidResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(svidResp.Body)
		return nil, fmt.Errorf("SVID issuance failed with status %d: %s", svidResp.StatusCode, string(body))
	}

	var svid SVIDResponse
	if err := json.NewDecoder(svidResp.Body).Decode(&svid); err != nil {
		return nil, fmt.Errorf("failed to decode SVID response: %w", err)
	}

	return &svid, nil
}

func (api *HeadlessAPI) VerifyCertificate(payload map[string]string) (*ValidationResult, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", api.BaseURL+"/api/v1/verify/certificate", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := api.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var result ValidationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode verification response: %w", err)
	}

	return &result, nil
}

// Close cleans up resources
func (s *SpiffeSDK) Close() error {
	s.cancel()
	if s.workloadAPI != nil {
		return s.workloadAPI.Close()
	}
	return nil
}