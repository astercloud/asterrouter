package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestAdminDepartmentEndpoints(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})

	parentBody := bytes.NewBufferString(`{"name":"Engineering","code":"eng","cost_center":"eng-core","monthly_budget_micros":250000,"status":"active"}`)
	parentReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/departments", parentBody)
	parentReq.Header.Set("Content-Type", "application/json")
	parentRec := httptest.NewRecorder()
	handler.ServeHTTP(parentRec, parentReq)
	if parentRec.Code != http.StatusOK {
		t.Fatalf("create parent status = %d body=%s", parentRec.Code, parentRec.Body.String())
	}
	var parentResp struct {
		Data controlplane.Department `json:"data"`
	}
	if err := json.Unmarshal(parentRec.Body.Bytes(), &parentResp); err != nil {
		t.Fatalf("decode parent: %v", err)
	}
	if parentResp.Data.ID == "" || parentResp.Data.Code != "ENG" || parentResp.Data.CostCenter != "ENG-CORE" {
		t.Fatalf("parent mismatch: %+v", parentResp.Data)
	}

	childBody := bytes.NewBufferString(`{"name":"Platform","code":"platform","parent_id":"` + parentResp.Data.ID + `","cost_center":"platform","monthly_budget_micros":120000,"status":"active"}`)
	childReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/departments", childBody)
	childReq.Header.Set("Content-Type", "application/json")
	childRec := httptest.NewRecorder()
	handler.ServeHTTP(childRec, childReq)
	if childRec.Code != http.StatusOK {
		t.Fatalf("create child status = %d body=%s", childRec.Code, childRec.Body.String())
	}
	var childResp struct {
		Data controlplane.Department `json:"data"`
	}
	if err := json.Unmarshal(childRec.Body.Bytes(), &childResp); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	if childResp.Data.ParentID != parentResp.Data.ID || childResp.Data.Code != "PLATFORM" {
		t.Fatalf("child mismatch: %+v", childResp.Data)
	}

	updateBody := bytes.NewBufferString(`{"name":"Platform Services","code":"platform","parent_id":"` + parentResp.Data.ID + `","cost_center":"platform-svc","monthly_budget_micros":160000,"status":"archived"}`)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/console/departments/"+childResp.Data.ID, updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update child status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp struct {
		Data controlplane.Department `json:"data"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updateResp.Data.Name != "Platform Services" || updateResp.Data.Status != controlplane.DepartmentStatusArchived || updateResp.Data.MonthlyBudgetMicros != 160000 {
		t.Fatalf("update mismatch: %+v", updateResp.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/departments", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.Department `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Data) != 2 {
		t.Fatalf("department list mismatch: %+v", listResp.Data)
	}

	duplicateReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/departments", bytes.NewBufferString(`{"name":"Duplicate","code":"ENG","status":"active"}`))
	duplicateReq.Header.Set("Content-Type", "application/json")
	duplicateRec := httptest.NewRecorder()
	handler.ServeHTTP(duplicateRec, duplicateReq)
	if duplicateRec.Code != http.StatusBadRequest || !strings.Contains(duplicateRec.Body.String(), "already exists") {
		t.Fatalf("duplicate code should be rejected status=%d body=%s", duplicateRec.Code, duplicateRec.Body.String())
	}

	missingParentReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/departments", bytes.NewBufferString(`{"name":"Unknown Parent","code":"unknown","parent_id":"dept_missing","status":"active"}`))
	missingParentReq.Header.Set("Content-Type", "application/json")
	missingParentRec := httptest.NewRecorder()
	handler.ServeHTTP(missingParentRec, missingParentReq)
	if missingParentRec.Code != http.StatusBadRequest || !strings.Contains(missingParentRec.Body.String(), "parent department") {
		t.Fatalf("missing parent should be rejected status=%d body=%s", missingParentRec.Code, missingParentRec.Body.String())
	}

	missingUpdateReq := httptest.NewRequest(http.MethodPut, "/api/v1/console/departments/missing", bytes.NewBufferString(`{"name":"Missing","code":"missing","status":"active"}`))
	missingUpdateReq.Header.Set("Content-Type", "application/json")
	missingUpdateRec := httptest.NewRecorder()
	handler.ServeHTTP(missingUpdateRec, missingUpdateReq)
	if missingUpdateRec.Code != http.StatusBadRequest || !strings.Contains(missingUpdateRec.Body.String(), "not found") {
		t.Fatalf("missing department update should be rejected status=%d body=%s", missingUpdateRec.Code, missingUpdateRec.Body.String())
	}

	negativeBudgetReq := httptest.NewRequest(http.MethodPut, "/api/v1/console/departments/"+childResp.Data.ID, bytes.NewBufferString(`{"name":"Platform Services","code":"platform","parent_id":"`+parentResp.Data.ID+`","monthly_budget_micros":-1,"status":"archived"}`))
	negativeBudgetReq.Header.Set("Content-Type", "application/json")
	negativeBudgetRec := httptest.NewRecorder()
	handler.ServeHTTP(negativeBudgetRec, negativeBudgetReq)
	if negativeBudgetRec.Code != http.StatusBadRequest || !strings.Contains(negativeBudgetRec.Body.String(), "greater than or equal to 0") {
		t.Fatalf("negative department budget should be rejected status=%d body=%s", negativeBudgetRec.Code, negativeBudgetRec.Body.String())
	}

	selfParentReq := httptest.NewRequest(http.MethodPut, "/api/v1/console/departments/"+childResp.Data.ID, bytes.NewBufferString(`{"name":"Platform Services","code":"platform","parent_id":"`+childResp.Data.ID+`","monthly_budget_micros":160000,"status":"archived"}`))
	selfParentReq.Header.Set("Content-Type", "application/json")
	selfParentRec := httptest.NewRecorder()
	handler.ServeHTTP(selfParentRec, selfParentReq)
	if selfParentRec.Code != http.StatusBadRequest || !strings.Contains(selfParentRec.Body.String(), "itself as parent") {
		t.Fatalf("self-parent department update should be rejected status=%d body=%s", selfParentRec.Code, selfParentRec.Body.String())
	}

	cycleReq := httptest.NewRequest(http.MethodPut, "/api/v1/console/departments/"+parentResp.Data.ID, bytes.NewBufferString(`{"name":"Engineering","code":"eng","parent_id":"`+childResp.Data.ID+`","monthly_budget_micros":250000,"status":"active"}`))
	cycleReq.Header.Set("Content-Type", "application/json")
	cycleRec := httptest.NewRecorder()
	handler.ServeHTTP(cycleRec, cycleReq)
	if cycleRec.Code != http.StatusBadRequest || !strings.Contains(cycleRec.Body.String(), "create a cycle") {
		t.Fatalf("cyclic department update should be rejected status=%d body=%s", cycleRec.Code, cycleRec.Body.String())
	}

	departments, err := control.ListDepartments(context.Background())
	if err != nil {
		t.Fatalf("ListDepartments(): %v", err)
	}
	if len(departments) != 2 {
		t.Fatalf("rejected department updates changed collection size: %+v", departments)
	}
	for _, department := range departments {
		switch department.ID {
		case parentResp.Data.ID:
			if department.ParentID != "" || department.MonthlyBudgetMicros != 250000 {
				t.Fatalf("rejected cycle mutated parent: %+v", department)
			}
		case childResp.Data.ID:
			if department.ParentID != parentResp.Data.ID || department.MonthlyBudgetMicros != 160000 || department.Status != controlplane.DepartmentStatusArchived {
				t.Fatalf("rejected update mutated child: %+v", department)
			}
		default:
			t.Fatalf("unexpected department after rejected updates: %+v", department)
		}
	}

	audit, err := control.ListAuditLogs(context.Background(), 20)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	var seenCreate, seenUpdate bool
	for _, event := range audit {
		seenCreate = seenCreate || event.ResourceType == "department" && event.Action == "create"
		seenUpdate = seenUpdate || event.ResourceType == "department" && event.Action == "update"
	}
	if !seenCreate || !seenUpdate {
		t.Fatalf("department audit events missing create=%v update=%v audit=%+v", seenCreate, seenUpdate, audit)
	}
}
