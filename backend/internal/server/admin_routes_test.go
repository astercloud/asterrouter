package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
	"github.com/gin-gonic/gin"
)

func TestAdminDashboardEndpoint(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/dashboard", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int                    `json:"code"`
		Data controlplane.Dashboard `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ProviderCount != 1 || resp.Data.APIKeyCount != 0 {
		t.Fatalf("unexpected dashboard: %+v", resp.Data)
	}
}

func TestAdminPricingRuleEndpoints(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})
	legacyReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/model-pricings", nil)
	legacyRec := httptest.NewRecorder()
	handler.ServeHTTP(legacyRec, legacyReq)
	if legacyRec.Code != http.StatusNotFound {
		t.Fatalf("legacy pricing status = %d body=%s", legacyRec.Code, legacyRec.Body.String())
	}
	invalidReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/pricing-rules", bytes.NewBufferString(`{"name":"invalid","amount_cents":1}`))
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidRec := httptest.NewRecorder()
	handler.ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("unknown cents field status = %d body=%s", invalidRec.Code, invalidRec.Body.String())
	}

	createBody := bytes.NewBufferString(`{"name":"Global usage","purpose":"usage_cost","scope_type":"global","scope_id":"","model":"*","currency":"USD","authoring_mode":"raw","expression":"v1: fixed_line(\"base\", \"request\", 120)","test_cases":[]}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/pricing-rules", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create pricing status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		Data controlplane.PricingRuleDetail `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create pricing: %v", err)
	}
	if createResp.Data.Rule.ID == "" || createResp.Data.Draft == nil || createResp.Data.Rule.Model != "*" {
		t.Fatalf("created pricing mismatch: %+v", createResp.Data)
	}
	publishBody := fmt.Sprintf(`{"draft_version_id":%q,"expected_lock_version":%d,"expected_active_version_id":"","expression_hash":%q}`, createResp.Data.Draft.ID, createResp.Data.Rule.LockVersion, createResp.Data.Draft.ExpressionHash)
	publishReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/pricing-rules/"+createResp.Data.Rule.ID+"/publish", strings.NewReader(publishBody))
	publishReq.Header.Set("Content-Type", "application/json")
	publishRec := httptest.NewRecorder()
	handler.ServeHTTP(publishRec, publishReq)
	if publishRec.Code != http.StatusOK {
		t.Fatalf("publish pricing status = %d body=%s", publishRec.Code, publishRec.Body.String())
	}
	staleReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/pricing-rules/"+createResp.Data.Rule.ID+"/publish", strings.NewReader(publishBody))
	staleReq.Header.Set("Content-Type", "application/json")
	staleRec := httptest.NewRecorder()
	handler.ServeHTTP(staleRec, staleReq)
	if staleRec.Code != http.StatusConflict {
		t.Fatalf("stale publish status = %d body=%s", staleRec.Code, staleRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/pricing-rules", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list pricing status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.PricingRule `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list pricing: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].ID != createResp.Data.Rule.ID {
		t.Fatalf("list pricing mismatch: %+v", listResp.Data)
	}
}

func TestAdminGatewayModelAndRouteEndpoints(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "route provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1",
		Status: controlplane.ProviderStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account := createGatewayTestAccount(t, control, provider, "upstream-chat", "account-secret", 10, 3)

	modelCreate := httptest.NewRequest(http.MethodPost, "/api/v1/console/gateway-models", bytes.NewBufferString(`{"model_id":"public-chat","name":"Public Chat","modality":"chat","default_route_group":"stable","status":"active"}`))
	modelCreate.Header.Set("Content-Type", "application/json")
	modelCreateRec := httptest.NewRecorder()
	handler.ServeHTTP(modelCreateRec, modelCreate)
	if modelCreateRec.Code != http.StatusOK {
		t.Fatalf("create gateway model status = %d body=%s", modelCreateRec.Code, modelCreateRec.Body.String())
	}
	var modelResp struct {
		Data controlplane.GatewayModel `json:"data"`
	}
	if err := json.Unmarshal(modelCreateRec.Body.Bytes(), &modelResp); err != nil {
		t.Fatalf("decode gateway model: %v", err)
	}
	modelUpdate := httptest.NewRequest(http.MethodPut, "/api/v1/console/gateway-models/"+modelResp.Data.ID, bytes.NewBufferString(`{"model_id":"public-chat","name":"Public Chat Updated","modality":"chat","default_route_group":"stable","status":"active"}`))
	modelUpdate.Header.Set("Content-Type", "application/json")
	modelUpdateRec := httptest.NewRecorder()
	handler.ServeHTTP(modelUpdateRec, modelUpdate)
	if modelUpdateRec.Code != http.StatusOK || !strings.Contains(modelUpdateRec.Body.String(), `"name":"Public Chat Updated"`) {
		t.Fatalf("update gateway model status = %d body=%s", modelUpdateRec.Code, modelUpdateRec.Body.String())
	}

	missingFormatBody := fmt.Sprintf(`{"gateway_model_id":%q,"route_group":"stable","provider_account_id":%q,"upstream_model":"upstream-chat","priority":10,"weight":100,"status":"active"}`, modelResp.Data.ID, account.ID)
	missingFormat := httptest.NewRequest(http.MethodPost, "/api/v1/console/model-routes", bytes.NewBufferString(missingFormatBody))
	missingFormat.Header.Set("Content-Type", "application/json")
	missingFormatRec := httptest.NewRecorder()
	handler.ServeHTTP(missingFormatRec, missingFormat)
	if missingFormatRec.Code != http.StatusBadRequest || !strings.Contains(missingFormatRec.Body.String(), "upstream_format is required") {
		t.Fatalf("missing format status = %d body=%s", missingFormatRec.Code, missingFormatRec.Body.String())
	}

	incompatibleFormatBody := fmt.Sprintf(`{"gateway_model_id":%q,"route_group":"stable","provider_account_id":%q,"upstream_model":"upstream-chat","upstream_format":"anthropic_messages","priority":10,"weight":100,"status":"active"}`, modelResp.Data.ID, account.ID)
	incompatibleFormat := httptest.NewRequest(http.MethodPost, "/api/v1/console/model-routes", bytes.NewBufferString(incompatibleFormatBody))
	incompatibleFormat.Header.Set("Content-Type", "application/json")
	incompatibleFormatRec := httptest.NewRecorder()
	handler.ServeHTTP(incompatibleFormatRec, incompatibleFormat)
	if incompatibleFormatRec.Code != http.StatusBadRequest || !strings.Contains(incompatibleFormatRec.Body.String(), "does not support gateway model modality") {
		t.Fatalf("incompatible format status = %d body=%s", incompatibleFormatRec.Code, incompatibleFormatRec.Body.String())
	}

	routeBody := fmt.Sprintf(`{"gateway_model_id":%q,"route_group":"stable","provider_account_id":%q,"upstream_model":"upstream-chat","upstream_format":"openai_chat","priority":10,"weight":100,"status":"active"}`, modelResp.Data.ID, account.ID)
	routeCreate := httptest.NewRequest(http.MethodPost, "/api/v1/console/model-routes", bytes.NewBufferString(routeBody))
	routeCreate.Header.Set("Content-Type", "application/json")
	routeCreateRec := httptest.NewRecorder()
	handler.ServeHTTP(routeCreateRec, routeCreate)
	if routeCreateRec.Code != http.StatusOK {
		t.Fatalf("create model route status = %d body=%s", routeCreateRec.Code, routeCreateRec.Body.String())
	}
	var routeResp struct {
		Data controlplane.ModelRoute `json:"data"`
	}
	if err := json.Unmarshal(routeCreateRec.Body.Bytes(), &routeResp); err != nil {
		t.Fatalf("decode model route: %v", err)
	}
	if routeResp.Data.UpstreamModel != "upstream-chat" || routeResp.Data.ProviderAccountID != account.ID || routeResp.Data.UpstreamFormat != controlplane.UpstreamFormatOpenAIChat {
		t.Fatalf("created route mismatch: %+v", routeResp.Data)
	}
	bulkModel, err := control.CreateGatewayModel(context.Background(), "tester", controlplane.GatewayModelRequest{ModelID: "public-bulk", Name: "Public Bulk", Modality: "chat", Status: controlplane.GatewayModelStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	bulkBody := fmt.Sprintf(`{"routes":[{"gateway_model_id":%q,"route_group":"stable","provider_account_id":%q,"upstream_model":"upstream-chat","upstream_format":"openai_chat","priority":30,"weight":100,"status":"active"}]}`, bulkModel.ID, account.ID)
	bulkCreate := httptest.NewRequest(http.MethodPost, "/api/v1/console/model-routes/bulk", bytes.NewBufferString(bulkBody))
	bulkCreate.Header.Set("Content-Type", "application/json")
	bulkCreateRec := httptest.NewRecorder()
	handler.ServeHTTP(bulkCreateRec, bulkCreate)
	if bulkCreateRec.Code != http.StatusOK || !strings.Contains(bulkCreateRec.Body.String(), `"routes"`) {
		t.Fatalf("bulk model route status = %d body=%s", bulkCreateRec.Code, bulkCreateRec.Body.String())
	}
	var bulkResp struct {
		Data controlplane.ModelRouteBulkCreateResult `json:"data"`
	}
	if err := json.Unmarshal(bulkCreateRec.Body.Bytes(), &bulkResp); err != nil || len(bulkResp.Data.Routes) != 1 {
		t.Fatalf("decode bulk model route response: data=%+v err=%v", bulkResp.Data, err)
	}

	modelList := httptest.NewRequest(http.MethodGet, "/api/v1/console/gateway-models", nil)
	modelListRec := httptest.NewRecorder()
	handler.ServeHTTP(modelListRec, modelList)
	if modelListRec.Code != http.StatusOK || !strings.Contains(modelListRec.Body.String(), `"route_count":1`) {
		t.Fatalf("gateway model list status = %d body=%s", modelListRec.Code, modelListRec.Body.String())
	}
	routeList := httptest.NewRequest(http.MethodGet, "/api/v1/console/model-routes", nil)
	routeListRec := httptest.NewRecorder()
	handler.ServeHTTP(routeListRec, routeList)
	if routeListRec.Code != http.StatusOK || !strings.Contains(routeListRec.Body.String(), routeResp.Data.ID) || !strings.Contains(routeListRec.Body.String(), bulkResp.Data.Routes[0].ID) {
		t.Fatalf("model route list status = %d body=%s", routeListRec.Code, routeListRec.Body.String())
	}

	routeUpdateBody := fmt.Sprintf(`{"gateway_model_id":%q,"route_group":"stable","provider_account_id":%q,"upstream_model":"upstream-chat","upstream_format":"openai_chat","priority":20,"weight":250,"status":"disabled"}`, modelResp.Data.ID, account.ID)
	routeUpdate := httptest.NewRequest(http.MethodPut, "/api/v1/console/model-routes/"+routeResp.Data.ID, bytes.NewBufferString(routeUpdateBody))
	routeUpdate.Header.Set("Content-Type", "application/json")
	routeUpdateRec := httptest.NewRecorder()
	handler.ServeHTTP(routeUpdateRec, routeUpdate)
	if routeUpdateRec.Code != http.StatusOK || !strings.Contains(routeUpdateRec.Body.String(), `"weight":250`) {
		t.Fatalf("update model route status = %d body=%s", routeUpdateRec.Code, routeUpdateRec.Body.String())
	}
	bulkDelete := httptest.NewRequest(http.MethodDelete, "/api/v1/console/model-routes/"+bulkResp.Data.Routes[0].ID, nil)
	bulkDeleteRec := httptest.NewRecorder()
	handler.ServeHTTP(bulkDeleteRec, bulkDelete)
	if bulkDeleteRec.Code != http.StatusOK {
		t.Fatalf("delete model route status = %d body=%s", bulkDeleteRec.Code, bulkDeleteRec.Body.String())
	}

	modelDelete := httptest.NewRequest(http.MethodDelete, "/api/v1/console/gateway-models/"+modelResp.Data.ID, nil)
	modelDeleteRec := httptest.NewRecorder()
	handler.ServeHTTP(modelDeleteRec, modelDelete)
	if modelDeleteRec.Code != http.StatusOK {
		t.Fatalf("delete gateway model status = %d body=%s", modelDeleteRec.Code, modelDeleteRec.Body.String())
	}
	routes, err := control.ListModelRoutes(context.Background())
	if err != nil || len(routes) != 0 {
		t.Fatalf("expected explicit and cascade route deletion to leave no routes: routes=%+v err=%v", routes, err)
	}
}

func TestAdminGatewayModelAndRouteMissingResourceContracts(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPut, path: "/api/v1/console/gateway-models/missing", body: `{"model_id":"missing","name":"Missing","status":"active"}`},
		{method: http.MethodDelete, path: "/api/v1/console/gateway-models/missing"},
		{method: http.MethodPut, path: "/api/v1/console/model-routes/missing", body: `{"gateway_model_id":"missing","provider_account_id":"missing","upstream_model":"missing","upstream_format":"openai_chat","status":"active"}`},
		{method: http.MethodDelete, path: "/api/v1/console/model-routes/missing"},
	}
	for _, test := range tests {
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		if test.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not found") {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminProviderAccountModelEndpoints(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"existing"},{"id":"new-model"}]}`))
	}))
	defer upstream.Close()
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{Name: "Inventory provider", Type: "openai_compatible", BaseURL: upstream.URL + "/v1", Status: controlplane.ProviderStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, control, provider, "existing", "account-secret", 10, 3)

	list := httptest.NewRequest(http.MethodGet, "/api/v1/console/provider-accounts/"+account.ID+"/models", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, list)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), `"model_id":"existing"`) {
		t.Fatalf("model inventory status = %d body=%s", listRec.Code, listRec.Body.String())
	}

	discover := httptest.NewRequest(http.MethodPost, "/api/v1/console/provider-accounts/"+account.ID+"/models/discover", nil)
	discoverRec := httptest.NewRecorder()
	handler.ServeHTTP(discoverRec, discover)
	if discoverRec.Code != http.StatusOK || !strings.Contains(discoverRec.Body.String(), `"new-model"`) {
		t.Fatalf("model discovery status = %d body=%s", discoverRec.Code, discoverRec.Body.String())
	}

	syncReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/provider-accounts/"+account.ID+"/models/sync", bytes.NewBufferString(`{"enabled_models":["existing","new-model"],"auto_enable_new_models":true}`))
	syncReq.Header.Set("Content-Type", "application/json")
	syncRec := httptest.NewRecorder()
	handler.ServeHTTP(syncRec, syncReq)
	if syncRec.Code != http.StatusOK || !strings.Contains(syncRec.Body.String(), `"auto_enable_new_models":true`) {
		t.Fatalf("model sync status = %d body=%s", syncRec.Code, syncRec.Body.String())
	}
}

func TestAdminProviderAccountModelEndpointsRejectMissingAccount(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/v1/console/provider-accounts/missing/models"},
		{method: http.MethodPost, path: "/api/v1/console/provider-accounts/missing/models/discover"},
		{method: http.MethodPost, path: "/api/v1/console/provider-accounts/missing/models/sync", body: `{"enabled_models":[]}`},
	}
	for _, test := range tests {
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		if test.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not found") {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminGovernancePolicyEndpoints(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	createBody := bytes.NewBufferString(`{"name":"Platform policy","scope_type":"global","model_allowlist":["gpt-4o-mini"],"model_denylist":[],"qps_limit":10,"monthly_token_limit":1000000,"monthly_budget_micros":50000,"overage_action":"block","prompt_logging_mode":"metadata_only","retention_days":30,"tool_call_allowed":true,"image_input_allowed":true,"web_access_allowed":false,"status":"active"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/policies", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create policy status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		Data controlplane.GovernancePolicy `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create policy: %v", err)
	}
	if createResp.Data.ID == "" || createResp.Data.Name != "Platform policy" || createResp.Data.QPSLimit != 10 || createResp.Data.Version != 1 {
		t.Fatalf("created policy mismatch: %+v", createResp.Data)
	}

	updateBody := bytes.NewBufferString(`{"name":"Platform policy updated","scope_type":"global","model_allowlist":[],"model_denylist":["legacy-model"],"qps_limit":0,"monthly_token_limit":0,"monthly_budget_micros":0,"overage_action":"warn","prompt_logging_mode":"disabled","retention_days":0,"tool_call_allowed":false,"image_input_allowed":true,"web_access_allowed":false,"status":"disabled"}`)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/console/policies/"+createResp.Data.ID, updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update policy status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp struct {
		Data controlplane.GovernancePolicy `json:"data"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("decode update policy: %v", err)
	}
	if updateResp.Data.Status != controlplane.GovernancePolicyStatusDisabled || updateResp.Data.OverageAction != controlplane.GovernancePolicyOverageWarn || updateResp.Data.Version != 2 {
		t.Fatalf("updated policy mismatch: %+v", updateResp.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/policies", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list policy status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.GovernancePolicy `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list policy: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].ID != createResp.Data.ID {
		t.Fatalf("list policy mismatch: %+v", listResp.Data)
	}

	missingReq := httptest.NewRequest(http.MethodPut, "/api/v1/console/policies/missing", bytes.NewBufferString(`{"name":"Missing","scope_type":"global","overage_action":"warn","prompt_logging_mode":"disabled","status":"active"}`))
	missingReq.Header.Set("Content-Type", "application/json")
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusBadRequest || !strings.Contains(missingRec.Body.String(), "not found") {
		t.Fatalf("missing policy update status = %d body=%s", missingRec.Code, missingRec.Body.String())
	}

	invalidReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/policies", bytes.NewBufferString(`{"name":"Invalid","scope_type":"customer","overage_action":"ignore","prompt_logging_mode":"full","status":"active"}`))
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidRec := httptest.NewRecorder()
	handler.ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid policy status = %d body=%s", invalidRec.Code, invalidRec.Body.String())
	}
}

func TestAdminRecordEndpointsSupportQueryParameters(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "query key",
		ModelAllowlist:    []string{"model-a", "model-b"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := control.AuthorizeGatewayModel(context.Background(), created.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-a", InputTokens: 1}); err != nil {
		t.Fatalf("RecordGatewayUsage a: %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{Model: "model-b", Status: "error", ProviderID: "provider-b", ErrorType: "policy_error", InputTokens: 2}); err != nil {
		t.Fatalf("RecordGatewayUsage b: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), auth, controlplane.GatewayTraceInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-a", ResponseSummary: "ok"}); err != nil {
		t.Fatalf("RecordGatewayTrace a: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), auth, controlplane.GatewayTraceInput{Model: "model-b", Status: "error", ProviderID: "provider-b", ErrorType: "policy_error", ResponseSummary: "blocked"}); err != nil {
		t.Fatalf("RecordGatewayTrace b: %v", err)
	}
	other, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "other query key",
		ModelAllowlist:    []string{"model-a"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey other(): %v", err)
	}
	otherAuth, err := control.AuthorizeGatewayModel(context.Background(), other.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel other(): %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), otherAuth, controlplane.GatewayUsageInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-other", InputTokens: 3}); err != nil {
		t.Fatalf("RecordGatewayUsage other: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), otherAuth, controlplane.GatewayTraceInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-other", ResponseSummary: "other"}); err != nil {
		t.Fatalf("RecordGatewayTrace other: %v", err)
	}
	if err := control.RecordGatewayCall(context.Background(), auth, "model-a", "forwarded", "Pagination query audit marker"); err != nil {
		t.Fatalf("RecordGatewayCall(): %v", err)
	}

	usageReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/usage?model=model-b&status=error&limit=1", nil)
	usageRec := httptest.NewRecorder()
	handler.ServeHTTP(usageRec, usageReq)
	if usageRec.Code != http.StatusOK {
		t.Fatalf("usage status = %d body=%s", usageRec.Code, usageRec.Body.String())
	}
	var usageResp struct {
		Data controlplane.UsageReport `json:"data"`
	}
	if err := json.Unmarshal(usageRec.Body.Bytes(), &usageResp); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if len(usageResp.Data.Recent) != 1 || usageResp.Data.Recent[0].Model != "model-b" || usageResp.Data.Recent[0].Status != "error" {
		t.Fatalf("usage query not applied: %+v", usageResp.Data.Recent)
	}

	usageKeyReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/usage?api_key_id="+url.QueryEscape(created.Record.ID)+"&limit=10", nil)
	usageKeyRec := httptest.NewRecorder()
	handler.ServeHTTP(usageKeyRec, usageKeyReq)
	if usageKeyRec.Code != http.StatusOK {
		t.Fatalf("usage key status = %d body=%s", usageKeyRec.Code, usageKeyRec.Body.String())
	}
	var usageKeyResp struct {
		Data controlplane.UsageReport `json:"data"`
	}
	if err := json.Unmarshal(usageKeyRec.Body.Bytes(), &usageKeyResp); err != nil {
		t.Fatalf("decode usage key: %v", err)
	}
	if len(usageKeyResp.Data.Recent) != 2 || usageKeyResp.Data.TotalRequests != 2 {
		t.Fatalf("usage api_key_id filter count mismatch: %+v", usageKeyResp.Data)
	}
	for _, record := range usageKeyResp.Data.Recent {
		if record.APIKeyID != created.Record.ID {
			t.Fatalf("usage api_key_id leaked another key: %+v", usageKeyResp.Data.Recent)
		}
	}

	costReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/cost-allocation?dimension=api_key&api_key_id="+url.QueryEscape(created.Record.ID), nil)
	costRec := httptest.NewRecorder()
	handler.ServeHTTP(costRec, costReq)
	if costRec.Code != http.StatusOK {
		t.Fatalf("cost allocation status = %d body=%s", costRec.Code, costRec.Body.String())
	}
	var costResp struct {
		Data controlplane.CostAllocationReport `json:"data"`
	}
	if err := json.Unmarshal(costRec.Body.Bytes(), &costResp); err != nil {
		t.Fatalf("decode cost allocation: %v", err)
	}
	if costResp.Data.Dimension != controlplane.CostAllocationByAPIKey || costResp.Data.TotalRequests != 2 || costResp.Data.TotalUsageCostMicros != 0 || len(costResp.Data.Rows) != 1 {
		t.Fatalf("cost allocation mismatch: %+v", costResp.Data)
	}
	if costResp.Data.Rows[0].APIKeyID != created.Record.ID || costResp.Data.Rows[0].APIKeyName != "query key" {
		t.Fatalf("cost allocation row mismatch: %+v", costResp.Data.Rows)
	}

	costBadReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/cost-allocation?dimension=project", nil)
	costBadRec := httptest.NewRecorder()
	handler.ServeHTTP(costBadRec, costBadReq)
	if costBadRec.Code != http.StatusBadRequest {
		t.Fatalf("cost allocation invalid dimension status = %d body=%s", costBadRec.Code, costBadRec.Body.String())
	}

	traceReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/gateway-traces?status=error&q=provider-b", nil)
	traceRec := httptest.NewRecorder()
	handler.ServeHTTP(traceRec, traceReq)
	if traceRec.Code != http.StatusOK {
		t.Fatalf("trace status = %d body=%s", traceRec.Code, traceRec.Body.String())
	}
	var traceResp struct {
		Data []controlplane.GatewayTrace `json:"data"`
	}
	if err := json.Unmarshal(traceRec.Body.Bytes(), &traceResp); err != nil {
		t.Fatalf("decode traces: %v", err)
	}
	if len(traceResp.Data) != 1 || traceResp.Data[0].ProviderID != "provider-b" || traceResp.Data[0].Status != "error" {
		t.Fatalf("trace query not applied: %+v", traceResp.Data)
	}

	traceKeyReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/gateway-traces?api_key_id="+url.QueryEscape(created.Record.ID)+"&limit=10", nil)
	traceKeyRec := httptest.NewRecorder()
	handler.ServeHTTP(traceKeyRec, traceKeyReq)
	if traceKeyRec.Code != http.StatusOK {
		t.Fatalf("trace key status = %d body=%s", traceKeyRec.Code, traceKeyRec.Body.String())
	}
	var traceKeyResp struct {
		Data []controlplane.GatewayTrace `json:"data"`
	}
	if err := json.Unmarshal(traceKeyRec.Body.Bytes(), &traceKeyResp); err != nil {
		t.Fatalf("decode trace key: %v", err)
	}
	if len(traceKeyResp.Data) != 2 {
		t.Fatalf("trace api_key_id filter count mismatch: %+v", traceKeyResp.Data)
	}
	for _, trace := range traceKeyResp.Data {
		if trace.APIKeyID != created.Record.ID {
			t.Fatalf("trace api_key_id leaked another key: %+v", traceKeyResp.Data)
		}
	}

	traceSummaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/gateway-traces/summary?limit=1", nil)
	traceSummaryRec := httptest.NewRecorder()
	handler.ServeHTTP(traceSummaryRec, traceSummaryReq)
	if traceSummaryRec.Code != http.StatusOK {
		t.Fatalf("trace summary status = %d body=%s", traceSummaryRec.Code, traceSummaryRec.Body.String())
	}
	var traceSummaryResp struct {
		Data controlplane.GatewayTraceSummary `json:"data"`
	}
	if err := json.Unmarshal(traceSummaryRec.Body.Bytes(), &traceSummaryResp); err != nil {
		t.Fatalf("decode trace summary: %v", err)
	}
	if traceSummaryResp.Data.Total != 3 || traceSummaryResp.Data.Routed != 3 || traceSummaryResp.Data.Errors != 1 {
		t.Fatalf("trace summary should ignore pagination and include matching records: %+v", traceSummaryResp.Data)
	}

	traceKeySummaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/gateway-traces/summary?api_key_id="+url.QueryEscape(created.Record.ID)+"&limit=1", nil)
	traceKeySummaryRec := httptest.NewRecorder()
	handler.ServeHTTP(traceKeySummaryRec, traceKeySummaryReq)
	if traceKeySummaryRec.Code != http.StatusOK {
		t.Fatalf("trace key summary status = %d body=%s", traceKeySummaryRec.Code, traceKeySummaryRec.Body.String())
	}
	var traceKeySummaryResp struct {
		Data controlplane.GatewayTraceSummary `json:"data"`
	}
	if err := json.Unmarshal(traceKeySummaryRec.Body.Bytes(), &traceKeySummaryResp); err != nil {
		t.Fatalf("decode trace key summary: %v", err)
	}
	if traceKeySummaryResp.Data.Total != 2 || traceKeySummaryResp.Data.Routed != 2 || traceKeySummaryResp.Data.Errors != 1 {
		t.Fatalf("trace key summary mismatch: %+v", traceKeySummaryResp.Data)
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/audit-logs?action=invoke&q=Pagination", nil)
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("audit status = %d body=%s", auditRec.Code, auditRec.Body.String())
	}
	var auditResp struct {
		Data []controlplane.AuditLog `json:"data"`
	}
	if err := json.Unmarshal(auditRec.Body.Bytes(), &auditResp); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(auditResp.Data) != 1 || auditResp.Data[0].Action != "invoke" || !strings.Contains(auditResp.Data[0].Summary, "Pagination") {
		t.Fatalf("audit query not applied: %+v", auditResp.Data)
	}

	auditSummaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/audit-logs/summary?action=invoke&limit=1", nil)
	auditSummaryRec := httptest.NewRecorder()
	handler.ServeHTTP(auditSummaryRec, auditSummaryReq)
	if auditSummaryRec.Code != http.StatusOK {
		t.Fatalf("audit summary status = %d body=%s", auditSummaryRec.Code, auditSummaryRec.Body.String())
	}
	var auditSummaryResp struct {
		Data controlplane.AuditLogSummary `json:"data"`
	}
	if err := json.Unmarshal(auditSummaryRec.Body.Bytes(), &auditSummaryResp); err != nil {
		t.Fatalf("decode audit summary: %v", err)
	}
	if auditSummaryResp.Data.Total != 1 || auditSummaryResp.Data.Actors != 1 || auditSummaryResp.Data.Resources != 1 || auditSummaryResp.Data.Actions != 1 {
		t.Fatalf("audit summary mismatch: %+v", auditSummaryResp.Data)
	}

	future := url.QueryEscape(time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))
	usageTimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/usage?from="+future, nil)
	usageTimeRec := httptest.NewRecorder()
	handler.ServeHTTP(usageTimeRec, usageTimeReq)
	if usageTimeRec.Code != http.StatusOK {
		t.Fatalf("usage time status = %d body=%s", usageTimeRec.Code, usageTimeRec.Body.String())
	}
	var usageTimeResp struct {
		Data controlplane.UsageReport `json:"data"`
	}
	if err := json.Unmarshal(usageTimeRec.Body.Bytes(), &usageTimeResp); err != nil {
		t.Fatalf("decode usage time: %v", err)
	}
	if len(usageTimeResp.Data.Recent) != 0 {
		t.Fatalf("usage time range not applied: %+v", usageTimeResp.Data.Recent)
	}

	traceTimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/gateway-traces?from="+future, nil)
	traceTimeRec := httptest.NewRecorder()
	handler.ServeHTTP(traceTimeRec, traceTimeReq)
	if traceTimeRec.Code != http.StatusOK {
		t.Fatalf("trace time status = %d body=%s", traceTimeRec.Code, traceTimeRec.Body.String())
	}
	var traceTimeResp struct {
		Data []controlplane.GatewayTrace `json:"data"`
	}
	if err := json.Unmarshal(traceTimeRec.Body.Bytes(), &traceTimeResp); err != nil {
		t.Fatalf("decode trace time: %v", err)
	}
	if len(traceTimeResp.Data) != 0 {
		t.Fatalf("trace time range not applied: %+v", traceTimeResp.Data)
	}

	auditTimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/audit-logs?from="+future, nil)
	auditTimeRec := httptest.NewRecorder()
	handler.ServeHTTP(auditTimeRec, auditTimeReq)
	if auditTimeRec.Code != http.StatusOK {
		t.Fatalf("audit time status = %d body=%s", auditTimeRec.Code, auditTimeRec.Body.String())
	}
	var auditTimeResp struct {
		Data []controlplane.AuditLog `json:"data"`
	}
	if err := json.Unmarshal(auditTimeRec.Body.Bytes(), &auditTimeResp); err != nil {
		t.Fatalf("decode audit time: %v", err)
	}
	if len(auditTimeResp.Data) != 0 {
		t.Fatalf("audit time range not applied: %+v", auditTimeResp.Data)
	}
}

func TestCreateAPIKeyEndpoint(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	body := bytes.NewBufferString(`{"name":"demo","model_allowlist":["gpt-4o-mini"],"qps_limit":2,"monthly_token_limit":1000}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/api-keys", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int                               `json:"code"`
		Data controlplane.APIKeyCreateResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Key == "" || resp.Data.Record.Fingerprint == "" {
		t.Fatalf("api key response incomplete: %+v", resp.Data)
	}
}

func TestAPIKeyPolicyExplanationEndpoint(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	policyBody := bytes.NewBufferString(`{"name":"Platform policy","scope_type":"global","model_allowlist":["gpt-4o-mini"],"qps_limit":5,"monthly_token_limit":1000,"overage_action":"block","prompt_logging_mode":"metadata_only","retention_days":30,"tool_call_allowed":true,"image_input_allowed":true,"web_access_allowed":false,"status":"active"}`)
	policyReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/policies", policyBody)
	policyReq.Header.Set("Content-Type", "application/json")
	policyRec := httptest.NewRecorder()
	handler.ServeHTTP(policyRec, policyReq)
	if policyRec.Code != http.StatusOK {
		t.Fatalf("create policy status = %d body=%s", policyRec.Code, policyRec.Body.String())
	}
	var policyResp struct {
		Data controlplane.GovernancePolicy `json:"data"`
	}
	if err := json.Unmarshal(policyRec.Body.Bytes(), &policyResp); err != nil {
		t.Fatalf("decode policy: %v", err)
	}

	keyBody := bytes.NewBufferString(`{"name":"demo","policy_id":"` + policyResp.Data.ID + `","model_allowlist":["gpt-4o-mini"],"qps_limit":2,"monthly_token_limit":1000}`)
	keyReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/api-keys", keyBody)
	keyReq.Header.Set("Content-Type", "application/json")
	keyRec := httptest.NewRecorder()
	handler.ServeHTTP(keyRec, keyReq)
	if keyRec.Code != http.StatusOK {
		t.Fatalf("create key status = %d body=%s", keyRec.Code, keyRec.Body.String())
	}
	var keyResp struct {
		Data controlplane.APIKeyCreateResponse `json:"data"`
	}
	if err := json.Unmarshal(keyRec.Body.Bytes(), &keyResp); err != nil {
		t.Fatalf("decode key: %v", err)
	}

	explainReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/api-keys/"+keyResp.Data.Record.ID+"/policy-explanation", nil)
	explainRec := httptest.NewRecorder()
	handler.ServeHTTP(explainRec, explainReq)
	if explainRec.Code != http.StatusOK {
		t.Fatalf("explain status = %d body=%s", explainRec.Code, explainRec.Body.String())
	}
	var explainResp struct {
		Data controlplane.GatewayPolicyExplanation `json:"data"`
	}
	if err := json.Unmarshal(explainRec.Body.Bytes(), &explainResp); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if explainResp.Data.SelectedPolicyID != policyResp.Data.ID || explainResp.Data.SelectedPolicyVersion != 1 || explainResp.Data.SelectedSource != controlplane.GatewayPolicySourceAPIKeyExplicit {
		t.Fatalf("explanation mismatch: %+v", explainResp.Data)
	}
	if len(explainResp.Data.Candidates) == 0 || !explainResp.Data.Candidates[0].Selected {
		t.Fatalf("explanation candidates mismatch: %+v", explainResp.Data.Candidates)
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/api-keys/missing/policy-explanation", nil)
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusBadRequest {
		t.Fatalf("missing key explanation status = %d body=%s", missingRec.Code, missingRec.Body.String())
	}
	var missingResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(missingRec.Body.Bytes(), &missingResp); err != nil {
		t.Fatalf("decode missing key explanation: %v", err)
	}
	if missingResp.Code != 1507 || !strings.Contains(missingResp.Message, "api key") || !strings.Contains(missingResp.Message, "not found") {
		t.Fatalf("missing key explanation contract mismatch: %+v", missingResp)
	}
}

func TestAdminSupplyCollectionsExposeEmptyArraysAndFirstModelDefaults(t *testing.T) {
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	router := gin.New()
	admin := router.Group("/api/v1/console")
	registerProviderAdminRoutes(admin, control)
	registerGatewayModelAdminRoutes(admin, control)
	registerEffectivePricingAdminRoutes(admin, control)

	for _, path := range []string{
		"/api/v1/console/providers",
		"/api/v1/console/provider-billing-sources",
		"/api/v1/console/provider-cache-capabilities",
		"/api/v1/console/gateway-models",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var response struct {
			Code int               `json:"code"`
			Data []json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode GET %s: %v", path, err)
		}
		if response.Code != 0 || response.Data == nil || len(response.Data) != 0 {
			t.Fatalf("GET %s must return a non-nil empty array: %+v body=%s", path, response, rec.Body.String())
		}
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/gateway-models", strings.NewReader(`{"model_id":"first-enterprise-model"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create first gateway model status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Data controlplane.GatewayModel `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode first gateway model: %v", err)
	}
	if created.Data.ModelID != "first-enterprise-model" || created.Data.Name != "first-enterprise-model" || created.Data.Modality != "chat" || created.Data.DefaultRouteGroup != controlplane.DefaultModelRouteGroup || created.Data.StickyTTLSeconds != 1800 || created.Data.Status != controlplane.GatewayModelStatusActive {
		t.Fatalf("first gateway model defaults mismatch: %+v", created.Data)
	}
	models, err := control.ListGatewayModels(t.Context())
	if err != nil {
		t.Fatalf("ListGatewayModels(): %v", err)
	}
	if len(models) != 1 || models[0].ID != created.Data.ID {
		t.Fatalf("first gateway model was not persisted: %+v", models)
	}
}

func TestProviderEndpointRejectsLegacyCredentialAndModelFields(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	createBody := bytes.NewBufferString(`{"name":"Vendor A","type":"openai_compatible","base_url":"https://example.com/v1","status":"active","models":["gpt-4o-mini"],"priority":10,"api_key":"sk-test-123456"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/providers", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusBadRequest {
		t.Fatalf("legacy create status = %d body=%s", createRec.Code, createRec.Body.String())
	}

	validBody := bytes.NewBufferString(`{"name":"Vendor A","type":"openai_compatible","base_url":"https://example.com/v1","status":"active","priority":10}`)
	validReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/providers", validBody)
	validReq.Header.Set("Content-Type", "application/json")
	validRec := httptest.NewRecorder()
	handler.ServeHTTP(validRec, validReq)
	if validRec.Code != http.StatusOK {
		t.Fatalf("valid create status = %d body=%s", validRec.Code, validRec.Body.String())
	}
	var createResp struct {
		Data controlplane.ProviderConnection `json:"data"`
	}
	if err := json.Unmarshal(validRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	updateBody := bytes.NewBufferString(`{"name":"Vendor A Updated","type":"openai_compatible","base_url":"https://example.com/v1","status":"active","models":["gpt-4o-mini"],"priority":20}`)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/console/providers/"+createResp.Data.ID, updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusBadRequest {
		t.Fatalf("legacy update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}

	validUpdate := httptest.NewRequest(http.MethodPut, "/api/v1/console/providers/"+createResp.Data.ID, bytes.NewBufferString(`{"name":"Vendor A Updated","type":"openai_compatible","base_url":"https://example.com/v2","status":"disabled","priority":20}`))
	validUpdate.Header.Set("Content-Type", "application/json")
	validUpdateRec := httptest.NewRecorder()
	handler.ServeHTTP(validUpdateRec, validUpdate)
	if validUpdateRec.Code != http.StatusOK || !strings.Contains(validUpdateRec.Body.String(), `"name":"Vendor A Updated"`) || !strings.Contains(validUpdateRec.Body.String(), `"status":"disabled"`) {
		t.Fatalf("valid update status = %d body=%s", validUpdateRec.Code, validUpdateRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/providers", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), createResp.Data.ID) || !strings.Contains(listRec.Body.String(), `"base_url":"https://example.com/v2"`) {
		t.Fatalf("provider list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
}

func TestCheckProviderEndpoint(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/providers/prov_openai_compatible/check", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data controlplane.ProviderHealthCheck `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ProviderID != "prov_openai_compatible" || resp.Data.Status == "" || resp.Data.Message == "" {
		t.Fatalf("incomplete check response: %+v", resp.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/provider-health-checks", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.ProviderHealthCheck `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].ProviderID != "prov_openai_compatible" {
		t.Fatalf("health list missing check: %+v", listResp.Data)
	}
	disabled, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "Disabled provider", Type: "openai_compatible", BaseURL: "https://disabled.example/v1", Status: controlplane.ProviderStatusDisabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	disabledReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/providers/"+disabled.ID+"/check", nil)
	disabledRec := httptest.NewRecorder()
	handler.ServeHTTP(disabledRec, disabledReq)
	if disabledRec.Code != http.StatusOK || !strings.Contains(disabledRec.Body.String(), `"status":"disabled"`) {
		t.Fatalf("disabled provider check status = %d body=%s", disabledRec.Code, disabledRec.Body.String())
	}
	missingReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/providers/missing/check", nil)
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusBadRequest || !strings.Contains(missingRec.Body.String(), "not found") {
		t.Fatalf("missing provider check status = %d body=%s", missingRec.Code, missingRec.Body.String())
	}
}

func TestAdminRoutingGroupsAndProviderAccountsEndpoints(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer account-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-account"}]}`))
	}))
	defer upstream.Close()

	groupBody := bytes.NewBufferString(`{"name":"OpenAI default","platform":"openai_compatible","rate_multiplier":1,"status":"active","sort_order":10}`)
	groupReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/routing-groups", groupBody)
	groupReq.Header.Set("Content-Type", "application/json")
	groupRec := httptest.NewRecorder()
	handler.ServeHTTP(groupRec, groupReq)
	if groupRec.Code != http.StatusOK {
		t.Fatalf("group status = %d body=%s", groupRec.Code, groupRec.Body.String())
	}
	var groupResp struct {
		Data controlplane.RoutingGroup `json:"data"`
	}
	if err := json.Unmarshal(groupRec.Body.Bytes(), &groupResp); err != nil {
		t.Fatalf("decode group: %v", err)
	}
	if groupResp.Data.ID == "" {
		t.Fatalf("group id missing: %+v", groupResp.Data)
	}

	groupListReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/routing-groups", nil)
	groupListRec := httptest.NewRecorder()
	handler.ServeHTTP(groupListRec, groupListReq)
	if groupListRec.Code != http.StatusOK || !strings.Contains(groupListRec.Body.String(), groupResp.Data.ID) {
		t.Fatalf("routing group list status = %d body=%s", groupListRec.Code, groupListRec.Body.String())
	}

	groupUpdateReq := httptest.NewRequest(http.MethodPut, "/api/v1/console/routing-groups/"+groupResp.Data.ID, bytes.NewBufferString(`{"name":"OpenAI stable","platform":"openai_compatible","group_type":"subscription","rate_multiplier":1,"monthly_budget_micros":5000000,"status":"active","sort_order":20}`))
	groupUpdateReq.Header.Set("Content-Type", "application/json")
	groupUpdateRec := httptest.NewRecorder()
	handler.ServeHTTP(groupUpdateRec, groupUpdateReq)
	if groupUpdateRec.Code != http.StatusOK || !strings.Contains(groupUpdateRec.Body.String(), `"name":"OpenAI stable"`) || !strings.Contains(groupUpdateRec.Body.String(), `"group_type":"subscription"`) {
		t.Fatalf("routing group update status = %d body=%s", groupUpdateRec.Code, groupUpdateRec.Body.String())
	}

	providerPayload := `{"name":"Account Provider","type":"openai_compatible","base_url":"` + upstream.URL + `/v1","status":"active","priority":10}`
	providerReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/providers", bytes.NewBufferString(providerPayload))
	providerReq.Header.Set("Content-Type", "application/json")
	providerRec := httptest.NewRecorder()
	handler.ServeHTTP(providerRec, providerReq)
	if providerRec.Code != http.StatusOK {
		t.Fatalf("provider status = %d body=%s", providerRec.Code, providerRec.Body.String())
	}
	var providerResp struct {
		Data controlplane.ProviderConnection `json:"data"`
	}
	if err := json.Unmarshal(providerRec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("decode provider: %v", err)
	}

	accountPayload := `{"provider_id":"` + providerResp.Data.ID + `","name":"Account A","platform":"openai_compatible","auth_type":"api_key","status":"active","schedulable":true,"priority":10,"concurrency":3,"rate_multiplier":1,"models":["gpt-4o-mini"],"group_ids":["` + groupResp.Data.ID + `"],"secret":"account-secret"}`
	accountReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/provider-accounts", bytes.NewBufferString(accountPayload))
	accountReq.Header.Set("Content-Type", "application/json")
	accountRec := httptest.NewRecorder()
	handler.ServeHTTP(accountRec, accountReq)
	if accountRec.Code != http.StatusOK {
		t.Fatalf("account status = %d body=%s", accountRec.Code, accountRec.Body.String())
	}
	var accountResp struct {
		Data controlplane.ProviderAccount `json:"data"`
	}
	if err := json.Unmarshal(accountRec.Body.Bytes(), &accountResp); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	if !accountResp.Data.SecretConfigured || accountResp.Data.SecretHint == "" {
		t.Fatalf("account secret metadata missing: %+v", accountResp.Data)
	}
	if accountResp.Data.ProviderID != providerResp.Data.ID {
		t.Fatalf("account provider binding missing: %+v", accountResp.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/provider-accounts", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.ProviderAccount `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].GroupIDs[0] != groupResp.Data.ID {
		t.Fatalf("unexpected account list: %+v", listResp.Data)
	}

	checkReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/provider-accounts/"+accountResp.Data.ID+"/check", nil)
	checkRec := httptest.NewRecorder()
	handler.ServeHTTP(checkRec, checkReq)
	if checkRec.Code != http.StatusOK {
		t.Fatalf("account check status = %d body=%s", checkRec.Code, checkRec.Body.String())
	}
	var checkResp struct {
		Data controlplane.ProviderAccountHealthCheck `json:"data"`
	}
	if err := json.Unmarshal(checkRec.Body.Bytes(), &checkResp); err != nil {
		t.Fatalf("decode account check: %v", err)
	}
	if checkResp.Data.Status != "ok" || checkResp.Data.AccountID != accountResp.Data.ID {
		t.Fatalf("unexpected account check: %+v", checkResp.Data)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/provider-account-health-checks", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("account health list status = %d body=%s", healthRec.Code, healthRec.Body.String())
	}
	var healthResp struct {
		Data []controlplane.ProviderAccountHealthCheck `json:"data"`
	}
	if err := json.Unmarshal(healthRec.Body.Bytes(), &healthResp); err != nil {
		t.Fatalf("decode account health list: %v", err)
	}
	if len(healthResp.Data) != 1 || healthResp.Data[0].AccountID != accountResp.Data.ID {
		t.Fatalf("account health list missing check: %+v", healthResp.Data)
	}

	accountUpdatePayload := `{"provider_id":"` + providerResp.Data.ID + `","name":"Account A Updated","platform":"openai_compatible","auth_type":"api_key","status":"active","schedulable":false,"priority":20,"concurrency":2,"rate_multiplier":1,"models":["gpt-4o-mini"],"group_ids":["` + groupResp.Data.ID + `"]}`
	accountUpdate := httptest.NewRequest(http.MethodPut, "/api/v1/console/provider-accounts/"+accountResp.Data.ID, bytes.NewBufferString(accountUpdatePayload))
	accountUpdate.Header.Set("Content-Type", "application/json")
	accountUpdateRec := httptest.NewRecorder()
	handler.ServeHTTP(accountUpdateRec, accountUpdate)
	if accountUpdateRec.Code != http.StatusOK || !strings.Contains(accountUpdateRec.Body.String(), `"name":"Account A Updated"`) || !strings.Contains(accountUpdateRec.Body.String(), `"secret_configured":true`) {
		t.Fatalf("account update status = %d body=%s", accountUpdateRec.Code, accountUpdateRec.Body.String())
	}

	accountDelete := httptest.NewRequest(http.MethodDelete, "/api/v1/console/provider-accounts/"+accountResp.Data.ID, nil)
	accountDeleteRec := httptest.NewRecorder()
	handler.ServeHTTP(accountDeleteRec, accountDelete)
	if accountDeleteRec.Code != http.StatusOK {
		t.Fatalf("account delete status = %d body=%s", accountDeleteRec.Code, accountDeleteRec.Body.String())
	}
	accounts, err := control.ListProviderAccounts(context.Background())
	if err != nil || len(accounts) != 0 {
		t.Fatalf("provider account deletion did not persist: accounts=%+v err=%v", accounts, err)
	}
}

func TestAdminProviderAccountMissingResourceContracts(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPut, path: "/api/v1/console/provider-accounts/missing", body: `{"provider_id":"missing","name":"Missing","platform":"openai_compatible","auth_type":"api_key","status":"active"}`},
		{method: http.MethodDelete, path: "/api/v1/console/provider-accounts/missing"},
		{method: http.MethodPost, path: "/api/v1/console/provider-accounts/missing/check"},
		{method: http.MethodPost, path: "/api/v1/console/provider-accounts/missing/clear-cooldown"},
	}
	for _, test := range tests {
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		if test.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not found") {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminRoutingGroupBoundaryContracts(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})
	tests := []struct {
		method string
		path   string
		body   string
		match  string
	}{
		{method: http.MethodPost, path: "/api/v1/console/routing-groups", body: `{"name":"No platform","status":"active"}`, match: "platform is required"},
		{method: http.MethodPost, path: "/api/v1/console/routing-groups", body: `{"name":"No budget","platform":"openai_compatible","group_type":"subscription","status":"active"}`, match: "budget limit"},
		{method: http.MethodPut, path: "/api/v1/console/routing-groups/missing", body: `{"name":"Missing","platform":"openai_compatible","status":"active"}`, match: "not found"},
	}
	for _, test := range tests {
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), test.match) {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminRoutingPolicyEndpoints(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})
	body := bytes.NewBufferString(`{"name":"Enterprise default","route_group":"default","status":"active","strategy":{"preset":"balanced","sticky_ttl_seconds":900,"failover_before_first_byte":true,"low_price_pool_mode":"auto"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/routing-policies", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create routing policy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data controlplane.RoutingPolicy `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode routing policy: %v", err)
	}
	if created.Data.ID == "" || created.Data.Version != 1 || !created.Data.Strategy.FailoverBeforeFirstByte {
		t.Fatalf("unexpected routing policy: %+v", created.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/routing-policies", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list routing policies status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed struct {
		Data []controlplane.RoutingPolicy `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode routing policies: %v", err)
	}
	if len(listed.Data) != 1 || listed.Data[0].ID != created.Data.ID {
		t.Fatalf("unexpected routing policy list: %+v", listed.Data)
	}

	updateBody := bytes.NewBufferString(`{"name":"Enterprise stable","route_group":"default","status":"active","strategy":{"preset":"stability","sticky_ttl_seconds":1200,"low_price_pool_mode":"none"}}`)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/console/routing-policies/"+created.Data.ID, updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update routing policy status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updated struct {
		Data controlplane.RoutingPolicy `json:"data"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated routing policy: %v", err)
	}
	if updated.Data.Version != 2 || updated.Data.Name != "Enterprise stable" || updated.Data.Strategy.Preset != controlplane.RoutingPolicyPresetStability {
		t.Fatalf("unexpected updated routing policy: %+v", updated.Data)
	}

	missingReq := httptest.NewRequest(http.MethodPut, "/api/v1/console/routing-policies/missing", bytes.NewBufferString(`{"name":"Missing","strategy":{"preset":"balanced","sticky_ttl_seconds":900}}`))
	missingReq.Header.Set("Content-Type", "application/json")
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusBadRequest {
		t.Fatalf("missing routing policy update status=%d body=%s", missingRec.Code, missingRec.Body.String())
	}

	badReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/routing-policies", bytes.NewBufferString(`{"name":"bad","route_group":"default","strategy":{"preset":"random","sticky_ttl_seconds":900}}`))
	badReq.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid routing policy status=%d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestAdminGatewaySimulatorContracts(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "simulator provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1", Status: controlplane.ProviderStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, control, provider, "upstream-chat", "account-secret", 10, 3)
	model, err := control.CreateGatewayModel(context.Background(), "tester", controlplane.GatewayModelRequest{
		ModelID: "simulated-chat", Name: "Simulated Chat", Modality: "chat", DefaultRouteGroup: "stable", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.CreateModelRoute(context.Background(), "tester", controlplane.ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: "stable", ProviderAccountID: account.ID, UpstreamModel: "upstream-chat",
		UpstreamFormat: controlplane.UpstreamFormatOpenAIChat, Priority: 10, Weight: 100, Status: controlplane.ModelRouteStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	readyReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/gateway-simulator", bytes.NewBufferString(`{"model":"simulated-chat","estimated_tokens":1000,"protocol":"openai_chat_completions","required_features":["text"]}`))
	readyReq.Header.Set("Content-Type", "application/json")
	readyRec := httptest.NewRecorder()
	handler.ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("ready simulation status=%d body=%s", readyRec.Code, readyRec.Body.String())
	}
	var ready struct {
		Data controlplane.GatewaySimulation `json:"data"`
	}
	if err := json.Unmarshal(readyRec.Body.Bytes(), &ready); err != nil {
		t.Fatal(err)
	}
	if ready.Data.Status != "ready" || ready.Data.ResolvedModel != "simulated-chat" || len(ready.Data.Candidates) != 1 || !ready.Data.Candidates[0].Eligible {
		t.Fatalf("unexpected ready simulation: %+v", ready.Data)
	}

	unresolvedReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/gateway-simulator", bytes.NewBufferString(`{"model":"missing-model","estimated_tokens":1000,"protocol":"openai_chat_completions"}`))
	unresolvedReq.Header.Set("Content-Type", "application/json")
	unresolvedRec := httptest.NewRecorder()
	handler.ServeHTTP(unresolvedRec, unresolvedReq)
	if unresolvedRec.Code != http.StatusOK || !strings.Contains(unresolvedRec.Body.String(), `"status":"unresolved"`) {
		t.Fatalf("unresolved simulation status=%d body=%s", unresolvedRec.Code, unresolvedRec.Body.String())
	}

	invalidReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/gateway-simulator", bytes.NewBufferString(`{"model":"simulated-chat","estimated_tokens":1000,"legacy_provider":"provider"}`))
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidRec := httptest.NewRecorder()
	handler.ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusBadRequest || !strings.Contains(invalidRec.Body.String(), "invalid gateway simulation payload") {
		t.Fatalf("invalid simulation status=%d body=%s", invalidRec.Code, invalidRec.Body.String())
	}
}

func TestAdminProviderAccountClearCooldownEndpoint(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "Cooldown provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  "active",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	schedulable := true
	account, err := control.CreateProviderAccount(context.Background(), "tester", controlplane.ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Cooldown account",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         controlplane.AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       10,
		Concurrency:    3,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		Secret:         "account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	if err := control.RecordProviderAccountFailure(context.Background(), account.ID, http.StatusInternalServerError, "upstream broke"); err != nil {
		t.Fatalf("RecordProviderAccountFailure(): %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/provider-accounts/"+account.ID+"/clear-cooldown", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data controlplane.ProviderAccount `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.CooldownUntil != nil {
		t.Fatalf("expected cooldown cleared: %+v", resp.Data)
	}

	audit, err := control.ListAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	var found bool
	for _, event := range audit {
		if event.ResourceType == "provider_account" && event.Action == "clear_cooldown" {
			found = true
		}
	}
	if !found {
		t.Fatalf("clear_cooldown audit event not found: %+v", audit)
	}
}

func TestAdminProviderAccountDeleteProtectsModelRoutes(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "Delete provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  controlplane.ProviderStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account := createGatewayTestAccount(t, control, provider, "upstream-model", "account-secret", 10, 3)
	model, err := control.CreateGatewayModel(context.Background(), "tester", controlplane.GatewayModelRequest{
		ModelID:  "delete-protected-model",
		Name:     "Delete Protected Model",
		Modality: "chat",
		Status:   controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateGatewayModel(): %v", err)
	}
	if _, err := control.CreateModelRoute(context.Background(), "tester", controlplane.ModelRouteRequest{
		GatewayModelID:    model.ID,
		RouteGroup:        controlplane.DefaultModelRouteGroup,
		ProviderAccountID: account.ID,
		UpstreamModel:     "upstream-model",
		UpstreamFormat:    controlplane.UpstreamFormatOpenAIChat,
		Status:            controlplane.ModelRouteStatusActive,
	}); err != nil {
		t.Fatalf("CreateModelRoute(): %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/console/provider-accounts/"+account.ID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Code != 1554 || !strings.Contains(resp.Message, "referenced by model route") {
		t.Fatalf("unexpected delete response: %+v", resp)
	}
}

func TestAdminSystemCheckUpdatesEndpoint(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/system/check-updates?force=true", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int               `json:"code"`
		Data system.UpdateInfo `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.CurrentVersion != "test" || resp.Data.Warning == "" {
		t.Fatalf("unexpected update info: %+v", resp.Data)
	}
	audit, err := control.ListAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	for _, event := range audit {
		if event.ResourceType == "system" && event.Action == "check_update" {
			return
		}
	}
	t.Fatalf("system update audit event not found: %+v", audit)
}

func TestAdminSystemUpdateWithoutManifestRequiresManualConfiguration(t *testing.T) {
	handler, _ := newTestRuntime(t, RuntimeConfig{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/system/update", nil)
	req.Header.Set("Idempotency-Key", "test-update")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "manifest") {
		t.Fatalf("expected manifest guidance: %s", rec.Body.String())
	}
}

func TestSystemBackupEndpointsExposeEmptyListAndRejectMemoryBackup(t *testing.T) {
	handler, _ := newTestRuntime(t, RuntimeConfig{})

	for _, path := range []string{"/api/v1/console/system/backups"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/system/backups", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "PostgreSQL") {
		t.Fatalf("POST backup status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/console/system/backups/restore", bytes.NewBufferString(`{"backup_id":"missing","confirm":false}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "confirmation") {
		t.Fatalf("POST restore status = %d body=%s", rec.Code, rec.Body.String())
	}

	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/v1/console/system/backups/s3/test"},
		{method: http.MethodGet, path: "/api/v1/console/system/backups/s3"},
		{method: http.MethodGet, path: "/api/v1/console/system/backups/s3/download?key=backups%2Fasterrouter-backup-missing.tar.gz"},
		{method: http.MethodPost, path: "/api/v1/console/system/backups/s3/restore", body: `{"key":"backups/asterrouter-backup-missing.tar.gz","confirm":true}`},
	} {
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		if test.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "S3 backup is not configured") {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, rec.Code, rec.Body.String())
		}
	}
}

func TestSystemBackupRoutesListAndDownloadStoredArchive(t *testing.T) {
	backupDir := t.TempDir()
	systemService := system.NewService(system.Config{Version: "test", BuildType: "source", BackupDir: backupDir})
	stored, err := systemService.StoreBackupArchive("asterrouter-backup-20260812T120000Z-test", strings.NewReader("synthetic-backup-content"))
	if err != nil {
		t.Fatalf("StoreBackupArchive(): %v", err)
	}
	router := gin.New()
	registerSystemRoutes(router.Group("/api/v1/console/system"), systemService, nil, nil)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/system/backups", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), stored.ID) || strings.Contains(listRec.Body.String(), backupDir) {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/system/backups/"+stored.ID+"/download", nil)
	downloadRec := httptest.NewRecorder()
	router.ServeHTTP(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK || downloadRec.Body.String() != "synthetic-backup-content" || !strings.Contains(downloadRec.Header().Get("Content-Disposition"), stored.Path) {
		t.Fatalf("download status=%d headers=%v body=%q", downloadRec.Code, downloadRec.Header(), downloadRec.Body.String())
	}

	for _, id := range []string{"asterrouter-backup-missing", "..%2F..%2Fsecret"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/console/system/backups/"+id+"/download", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("download %q status=%d body=%s", id, rec.Code, rec.Body.String())
		}
	}
}

func TestSystemS3BackupRoutesTestListDownloadAndRestore(t *testing.T) {
	const (
		backupID  = "asterrouter-backup-20260812T120000Z-s3test"
		backupKey = "backups/" + backupID + ".tar.gz"
	)
	archive := testSystemBackupArchive(t)
	lastModified := "2026-08-12T12:00:00Z"
	requestCounts := map[string]int{}
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Errorf("unsigned S3 request: %s %s", r.Method, r.URL.String())
		}
		switch {
		case r.Method == http.MethodHead && r.URL.Path == "/test-bucket":
			requestCounts["head"]++
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/test-bucket" && r.URL.Query().Get("list-type") == "2":
			requestCounts["list"]++
			if r.URL.Query().Get("prefix") != "backups/" {
				t.Errorf("list prefix = %q", r.URL.Query().Get("prefix"))
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>test-bucket</Name><Prefix>backups/</Prefix><KeyCount>2</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated>
  <Contents><Key>%s</Key><LastModified>%s</LastModified><ETag>&quot;backup-etag&quot;</ETag><Size>%d</Size><StorageClass>STANDARD</StorageClass></Contents>
  <Contents><Key>backups/readme.txt</Key><LastModified>%s</LastModified><ETag>&quot;ignored-etag&quot;</ETag><Size>7</Size><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`, backupKey, lastModified, len(archive), lastModified)
		case r.Method == http.MethodGet && r.URL.Path == "/test-bucket/"+backupKey:
			requestCounts["download"]++
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(archive)
		default:
			http.Error(w, "unexpected S3 request", http.StatusNotFound)
		}
	}))
	defer s3Server.Close()

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "pg_restore"), []byte("#!/bin/sh\nset -eu\nexit 0\n"), 0750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	settingsRepo := settings.NewMemoryRepository()
	if err := settingsRepo.SetMultiple(t.Context(), map[string]string{
		settings.KeyBackupS3Enabled:   "true",
		settings.KeyBackupS3Endpoint:  s3Server.URL,
		settings.KeyBackupS3Region:    "test",
		settings.KeyBackupS3Bucket:    "test-bucket",
		settings.KeyBackupS3Prefix:    "backups",
		settings.KeyBackupS3AccessKey: "test-access",
		settings.KeyBackupS3SecretKey: "test-secret",
		settings.KeyBackupS3PathStyle: "true",
	}); err != nil {
		t.Fatalf("configure S3 backup: %v", err)
	}
	settingsService := settings.NewService(settingsRepo, settings.ServiceOptions{Version: "test", StorageMode: "memory"})
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := control.EnsureSeedData(t.Context()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	systemService := system.NewService(system.Config{
		Version: "test", BuildType: "source", DatabaseURL: "postgres://test.invalid/router", BackupDir: filepath.Join(root, "backups"),
	})
	router := gin.New()
	registerSystemRoutes(router.Group("/api/v1/console/system"), systemService, settingsService, control)

	testReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/system/backups/s3/test", nil)
	testRec := httptest.NewRecorder()
	router.ServeHTTP(testRec, testReq)
	if testRec.Code != http.StatusOK || !strings.Contains(testRec.Body.String(), `"connected":true`) {
		t.Fatalf("connection test status=%d body=%s", testRec.Code, testRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/system/backups/s3", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed struct {
		Data []system.S3BackupObject `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode S3 backup list: %v", err)
	}
	if len(listed.Data) != 1 || listed.Data[0].ID != backupID || listed.Data[0].Key != backupKey || listed.Data[0].SizeBytes != int64(len(archive)) {
		t.Fatalf("S3 backup list mismatch: %+v", listed.Data)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/system/backups/s3/download?key="+url.QueryEscape(backupKey), nil)
	downloadRec := httptest.NewRecorder()
	router.ServeHTTP(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK || !bytes.Equal(downloadRec.Body.Bytes(), archive) || !strings.Contains(downloadRec.Header().Get("Content-Disposition"), backupID+".tar.gz") {
		t.Fatalf("download status=%d headers=%v size=%d", downloadRec.Code, downloadRec.Header(), downloadRec.Body.Len())
	}

	invalidDownloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/system/backups/s3/download?key="+url.QueryEscape("backups/../secret.tar.gz"), nil)
	invalidDownloadRec := httptest.NewRecorder()
	router.ServeHTTP(invalidDownloadRec, invalidDownloadReq)
	if invalidDownloadRec.Code != http.StatusBadRequest || !strings.Contains(invalidDownloadRec.Body.String(), "S3 backup key is invalid") {
		t.Fatalf("invalid key status=%d body=%s", invalidDownloadRec.Code, invalidDownloadRec.Body.String())
	}

	unconfirmedReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/system/backups/s3/restore", strings.NewReader(`{"key":"`+backupKey+`","confirm":false}`))
	unconfirmedReq.Header.Set("Content-Type", "application/json")
	unconfirmedRec := httptest.NewRecorder()
	router.ServeHTTP(unconfirmedRec, unconfirmedReq)
	if unconfirmedRec.Code != http.StatusConflict || !strings.Contains(unconfirmedRec.Body.String(), "explicit confirmation") {
		t.Fatalf("unconfirmed restore status=%d body=%s", unconfirmedRec.Code, unconfirmedRec.Body.String())
	}

	restoreReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/system/backups/s3/restore", strings.NewReader(`{"key":"`+backupKey+`","confirm":true}`))
	restoreReq.Header.Set("Content-Type", "application/json")
	restoreRec := httptest.NewRecorder()
	router.ServeHTTP(restoreRec, restoreReq)
	if restoreRec.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", restoreRec.Code, restoreRec.Body.String())
	}
	var restored struct {
		Data system.RestoreResult `json:"data"`
	}
	if err := json.Unmarshal(restoreRec.Body.Bytes(), &restored); err != nil {
		t.Fatalf("decode S3 restore: %v", err)
	}
	if restored.Data.BackupID != backupID || !restored.Data.NeedRestart || !strings.HasPrefix(restored.Data.OperationID, "sys_restore-s3_") {
		t.Fatalf("S3 restore mismatch: %+v", restored.Data)
	}
	if requestCounts["head"] != 1 || requestCounts["list"] != 1 || requestCounts["download"] != 2 {
		t.Fatalf("S3 request counts: %+v", requestCounts)
	}
	audit, err := control.ListAuditLogs(t.Context(), 20)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	for _, event := range audit {
		if event.ResourceType == "system" && event.Action == "restore_s3" && event.ResourceID == backupID {
			return
		}
	}
	t.Fatalf("S3 restore audit event not found: %+v", audit)
}

func testSystemBackupArchive(t *testing.T) []byte {
	t.Helper()
	files := []struct {
		name    string
		content []byte
	}{
		{name: "database.dump", content: []byte("synthetic-postgres-dump")},
		{name: "manifest.json", content: []byte(`{"schema_version":"asterrouter.archive.v1","kind":"backup","database_format":"pg_dump_custom","database_included":true,"plugin_cache_included":false,"plugin_active_included":false}`)},
	}
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range files {
		if err := tarWriter.WriteHeader(&tar.Header{Name: file.name, Mode: 0600, Size: int64(len(file.content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("write backup %s header: %v", file.name, err)
		}
		if _, err := tarWriter.Write(file.content); err != nil {
			t.Fatalf("write backup %s: %v", file.name, err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close backup tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close backup gzip: %v", err)
	}
	return archive.Bytes()
}

func TestSystemRoutesRequireAuthentication(t *testing.T) {
	handler, _ := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/v1/console/system/check-updates"},
		{method: http.MethodPost, path: "/api/v1/console/system/update"},
		{method: http.MethodPost, path: "/api/v1/console/system/rollback"},
		{method: http.MethodPost, path: "/api/v1/console/system/restart"},
		{method: http.MethodGet, path: "/api/v1/console/system/backups"},
		{method: http.MethodPost, path: "/api/v1/console/system/backups"},
		{method: http.MethodPost, path: "/api/v1/console/system/backups/s3/test"},
		{method: http.MethodGet, path: "/api/v1/console/system/backups/s3"},
		{method: http.MethodGet, path: "/api/v1/console/system/backups/s3/download?key=backups%2Fasterrouter-backup-missing.tar.gz"},
		{method: http.MethodPost, path: "/api/v1/console/system/backups/s3/restore", body: `{"key":"backups/asterrouter-backup-missing.tar.gz","confirm":true}`},
		{method: http.MethodGet, path: "/api/v1/console/system/backups/asterrouter-backup-missing/download"},
		{method: http.MethodPost, path: "/api/v1/console/system/backups/restore", body: `{"backup_id":"missing","confirm":true}`},
		{method: http.MethodPost, path: "/api/v1/console/system/diagnostics"},
		{method: http.MethodGet, path: "/api/v1/console/system/diagnostics/asterrouter-diagnostic-missing/download"},
	}
	for _, test := range tests {
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		if test.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, rec.Code, rec.Body.String())
		}
	}
}

func TestSystemDiagnosticRoutesCreateAndDownload(t *testing.T) {
	diagnosticDir := t.TempDir()
	control := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	if err := control.EnsureSeedData(t.Context()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	router := http.NewServeMux()
	ginRouter := gin.New()
	registerSystemRoutes(
		ginRouter.Group("/api/v1/console/system"),
		system.NewService(system.Config{Version: "test", BuildType: "source", DiagnosticDir: diagnosticDir}),
		nil,
		control,
	)
	router.Handle("/", ginRouter)

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/system/diagnostics", nil)
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Code int                   `json:"code"`
		Data system.DiagnosticInfo `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Code != 0 || created.Data.ID == "" || created.Data.SizeBytes <= 0 || created.Data.Path != created.Data.ID+".tar.gz" {
		t.Fatalf("unexpected diagnostic response: %+v", created)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/system/diagnostics/"+created.Data.ID+"/download", nil)
	downloadRec := httptest.NewRecorder()
	router.ServeHTTP(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download status = %d body=%s", downloadRec.Code, downloadRec.Body.String())
	}
	if got := downloadRec.Header().Get("Content-Disposition"); !strings.Contains(got, created.Data.Path) {
		t.Fatalf("Content-Disposition = %q, want filename %q", got, created.Data.Path)
	}
	if int64(downloadRec.Body.Len()) != created.Data.SizeBytes {
		t.Fatalf("download size = %d, want %d", downloadRec.Body.Len(), created.Data.SizeBytes)
	}
	audit, err := control.ListAuditLogs(t.Context(), 10)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	for _, event := range audit {
		if event.ResourceType == "system" && event.Action == "diagnostic" && event.ResourceID == created.Data.ID {
			return
		}
	}
	t.Fatalf("diagnostic audit event not found: %+v", audit)
}

func TestSystemDiagnosticRoutesRejectUnavailableServiceAndInvalidArchiveID(t *testing.T) {
	withoutService := gin.New()
	registerSystemRoutes(withoutService.Group("/api/v1/console/system"), nil, nil, nil)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/console/system/diagnostics", nil)
	createRec := httptest.NewRecorder()
	withoutService.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable create status = %d body=%s", createRec.Code, createRec.Body.String())
	}

	withService := gin.New()
	registerSystemRoutes(
		withService.Group("/api/v1/console/system"),
		system.NewService(system.Config{Version: "test", BuildType: "source", DiagnosticDir: t.TempDir()}),
		nil,
		nil,
	)
	for _, id := range []string{"asterrouter-diagnostic-missing", "..%2F..%2Fsecret"} {
		downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/console/system/diagnostics/"+id+"/download", nil)
		downloadRec := httptest.NewRecorder()
		withService.ServeHTTP(downloadRec, downloadReq)
		if downloadRec.Code != http.StatusNotFound {
			t.Fatalf("download %q status = %d body=%s", id, downloadRec.Code, downloadRec.Body.String())
		}
	}
}
