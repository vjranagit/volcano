package webhook

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestValidateJobGroup_Valid(t *testing.T) {
	server := NewServer(8443, "", "", slog.Default())

	spec := map[string]interface{}{
		"apiVersion": "scheduling.volcano.sh/v1alpha1",
		"kind":       "JobGroup",
		"metadata": map[string]interface{}{
			"name":      "test-group",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"minMember":               3,
			"maxMember":               6,
			"scheduleTimeoutSeconds":  600,
			"priority":                100,
		},
	}

	raw, _ := json.Marshal(spec)
	req := &admissionv1.AdmissionRequest{
		UID:       "test-uid",
		Name:      "test-group",
		Namespace: "default",
		Object: runtime.RawExtension{
			Raw: raw,
		},
	}

	response := server.validateJobGroup(req)
	assert.True(t, response.Allowed)
	assert.Nil(t, response.Result)
}

func TestValidateJobGroup_InvalidMinMember(t *testing.T) {
	server := NewServer(8443, "", "", slog.Default())

	spec := map[string]interface{}{
		"spec": map[string]interface{}{
			"minMember":              0,
			"scheduleTimeoutSeconds": 600,
		},
	}

	raw, _ := json.Marshal(spec)
	req := &admissionv1.AdmissionRequest{
		UID: "test-uid",
		Object: runtime.RawExtension{
			Raw: raw,
		},
	}

	response := server.validateJobGroup(req)
	assert.False(t, response.Allowed)
	assert.Contains(t, response.Result.Message, "minMember must be positive")
}

func TestValidateJobGroup_InvalidMaxMember(t *testing.T) {
	server := NewServer(8443, "", "", slog.Default())

	spec := map[string]interface{}{
		"spec": map[string]interface{}{
			"minMember":              5,
			"maxMember":              3,
			"scheduleTimeoutSeconds": 600,
		},
	}

	raw, _ := json.Marshal(spec)
	req := &admissionv1.AdmissionRequest{
		UID: "test-uid",
		Object: runtime.RawExtension{
			Raw: raw,
		},
	}

	response := server.validateJobGroup(req)
	assert.False(t, response.Allowed)
	assert.Contains(t, response.Result.Message, "maxMember must be >= minMember")
}

func TestMutateJobGroup_AppliesDefaults(t *testing.T) {
	server := NewServer(8443, "", "", slog.Default())

	spec := map[string]interface{}{
		"spec": map[string]interface{}{
			"minMember": 3,
		},
	}

	raw, _ := json.Marshal(spec)
	req := &admissionv1.AdmissionRequest{
		UID: "test-uid",
		Object: runtime.RawExtension{
			Raw: raw,
		},
	}

	response := server.mutateJobGroup(req)
	assert.True(t, response.Allowed)
	assert.NotNil(t, response.Patch)
	assert.Equal(t, admissionv1.PatchTypeJSONPatch, *response.PatchType)
}

func TestHandleHealth(t *testing.T) {
	server := NewServer(8443, "", "", slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.handleHealth(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestParseAdmissionReview_InvalidMethod(t *testing.T) {
	server := NewServer(8443, "", "", slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/validate", nil)
	_, err := server.parseAdmissionReview(req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid method")
}

func TestParseAdmissionReview_Valid(t *testing.T) {
	server := NewServer(8443, "", "", slog.Default())

	review := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
		},
	}

	body, _ := json.Marshal(review)
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))

	parsed, err := server.parseAdmissionReview(req)
	require.NoError(t, err)
	assert.Equal(t, "test-uid", string(parsed.Request.UID))
}
