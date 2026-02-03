package webhook

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

func init() {
	_ = admissionv1.AddToScheme(scheme)
}

// Server is the admission webhook server.
type Server struct {
	port     int
	certFile string
	keyFile  string
	logger   *slog.Logger
	server   *http.Server
}

// NewServer creates a new webhook server.
func NewServer(port int, certFile, keyFile string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	return &Server{
		port:     port,
		certFile: certFile,
		keyFile:  keyFile,
		logger:   logger,
	}
}

// Run starts the webhook server.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/validate", s.handleValidate)
	mux.HandleFunc("/mutate", s.handleMutate)
	mux.HandleFunc("/health", s.handleHealth)

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("webhook server listening", "port", s.port)
		if err := s.server.ListenAndServeTLS(s.certFile, s.keyFile); err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return s.server.Shutdown(context.Background())
	}
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	s.logger.Debug("received validation request")

	review, err := s.parseAdmissionReview(r)
	if err != nil {
		s.logger.Error("failed to parse admission review", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := s.validateJobGroup(review.Request)
	review.Response = response

	s.writeResponse(w, review)
}

func (s *Server) handleMutate(w http.ResponseWriter, r *http.Request) {
	s.logger.Debug("received mutation request")

	review, err := s.parseAdmissionReview(r)
	if err != nil {
		s.logger.Error("failed to parse admission review", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := s.mutateJobGroup(review.Request)
	review.Response = response

	s.writeResponse(w, review)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) parseAdmissionReview(r *http.Request) (*admissionv1.AdmissionReview, error) {
	if r.Method != http.MethodPost {
		return nil, fmt.Errorf("invalid method: %s", r.Method)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}
	defer r.Body.Close()

	review := &admissionv1.AdmissionReview{}
	if _, _, err := codecs.UniversalDeserializer().Decode(body, nil, review); err != nil {
		return nil, fmt.Errorf("failed to decode body: %w", err)
	}

	return review, nil
}

func (s *Server) writeResponse(w http.ResponseWriter, review *admissionv1.AdmissionReview) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(review); err != nil {
		s.logger.Error("failed to encode response", "error", err)
	}
}

func (s *Server) validateJobGroup(req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	response := &admissionv1.AdmissionResponse{
		UID:     req.UID,
		Allowed: true,
	}

	var spec map[string]interface{}
	if err := json.Unmarshal(req.Object.Raw, &spec); err != nil {
		response.Allowed = false
		response.Result = &metav1.Status{
			Message: fmt.Sprintf("failed to unmarshal spec: %v", err),
		}
		return response
	}

	specData, ok := spec["spec"].(map[string]interface{})
	if !ok {
		response.Allowed = false
		response.Result = &metav1.Status{
			Message: "spec field is required",
		}
		return response
	}

	// Validate minMember
	minMember, _ := specData["minMember"].(float64)
	if minMember <= 0 {
		response.Allowed = false
		response.Result = &metav1.Status{
			Message: "minMember must be positive",
		}
		return response
	}

	// Validate maxMember
	maxMember, _ := specData["maxMember"].(float64)
	if maxMember > 0 && maxMember < minMember {
		response.Allowed = false
		response.Result = &metav1.Status{
			Message: "maxMember must be >= minMember",
		}
		return response
	}

	// Validate scheduleTimeoutSeconds
	timeout, _ := specData["scheduleTimeoutSeconds"].(float64)
	if timeout <= 0 {
		response.Allowed = false
		response.Result = &metav1.Status{
			Message: "scheduleTimeoutSeconds must be positive",
		}
		return response
	}

	s.logger.Info("validation passed", "namespace", req.Namespace, "name", req.Name)
	return response
}

func (s *Server) mutateJobGroup(req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	response := &admissionv1.AdmissionResponse{
		UID:     req.UID,
		Allowed: true,
	}

	var spec map[string]interface{}
	if err := json.Unmarshal(req.Object.Raw, &spec); err != nil {
		s.logger.Error("failed to unmarshal for mutation", "error", err)
		return response
	}

	specData, ok := spec["spec"].(map[string]interface{})
	if !ok {
		return response
	}

	modified := false

	// Set default maxMember if not specified
	if _, exists := specData["maxMember"]; !exists {
		minMember, _ := specData["minMember"].(float64)
		specData["maxMember"] = minMember * 2
		modified = true
	}

	// Set default priority if not specified
	if _, exists := specData["priority"]; !exists {
		specData["priority"] = 50
		modified = true
	}

	// Set default timeout if not specified
	if _, exists := specData["scheduleTimeoutSeconds"]; !exists {
		specData["scheduleTimeoutSeconds"] = 600
		modified = true
	}

	if modified {
		patchedSpec, _ := json.Marshal(spec)
		patch := []byte(fmt.Sprintf(`[{"op":"replace","path":"/spec","value":%s}]`, string(patchedSpec)))
		response.Patch = patch
		patchType := admissionv1.PatchTypeJSONPatch
		response.PatchType = &patchType

		s.logger.Info("applied default values", "namespace", req.Namespace, "name", req.Name)
	}

	return response
}
