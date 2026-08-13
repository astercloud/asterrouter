package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestApplicationEndpointsPersistLifecycleAndRejectInvalidUpdates(t *testing.T) {
	handler, _ := newTestRuntime(t, RuntimeConfig{})

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewBufferString(`{"name":"Customer Service","slug":"customer-service","entitlement_reference":"plan-enterprise","concurrency_limit":8,"status":"active"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Data controlplane.Application `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Data.ID == "" || created.Data.Slug != "customer-service" || created.Data.ConcurrencyLimit != 8 {
		t.Fatalf("created application=%+v", created.Data)
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+created.Data.ID, bytes.NewBufferString(`{"name":"Customer Service Production","slug":"customer-service","entitlement_reference":"plan-enterprise-v2","concurrency_limit":12,"status":"disabled"}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updated struct {
		Data controlplane.Application `json:"data"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updated.Data.ID != created.Data.ID || updated.Data.Name != "Customer Service Production" || updated.Data.Status != controlplane.ApplicationStatusDisabled || updated.Data.ConcurrencyLimit != 12 || !updated.Data.CreatedAt.Equal(created.Data.CreatedAt) {
		t.Fatalf("updated application=%+v created=%+v", updated.Data, created.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed struct {
		Data []controlplane.Application `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, application := range listed.Data {
		if application.ID == created.Data.ID {
			found = application.Name == updated.Data.Name && application.Status == updated.Data.Status && application.EntitlementReference == "plan-enterprise-v2"
		}
	}
	if !found {
		t.Fatalf("persisted application missing from list: %+v", listed.Data)
	}

	for name, test := range map[string]struct {
		id   string
		body string
		want string
	}{
		"missing application":  {id: "missing", body: `{"name":"Missing","slug":"missing","status":"active"}`, want: "not found"},
		"negative concurrency": {id: created.Data.ID, body: `{"name":"Invalid","slug":"customer-service","concurrency_limit":-1,"status":"active"}`, want: "non-negative"},
		"invalid status":       {id: created.Data.ID, body: `{"name":"Invalid","slug":"customer-service","status":"archived"}`, want: "active or disabled"},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+test.id, strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), test.want) {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
