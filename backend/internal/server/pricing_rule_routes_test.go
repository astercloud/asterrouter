package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/pricing"
	"github.com/gin-gonic/gin"
)

func TestPricingRuleRoutesOnlyAcceptEnterpriseUsageCostRules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1")
	router := gin.New()
	registerPricingRuleRoutes(router.Group("/costs"), service)

	valid := httptest.NewRequest(http.MethodPost, "/costs/pricing-rules", bytes.NewBufferString(`{"name":"usage cost","purpose":"usage_cost","scope_type":"global","scope_id":"","model":"*","currency":"USD","authoring_mode":"raw","expression":"v1: fixed_line(\"request\", \"request\", 1)","test_cases":[]}`))
	valid.Header.Set("Content-Type", "application/json")
	validRecorder := httptest.NewRecorder()
	router.ServeHTTP(validRecorder, valid)
	if validRecorder.Code != http.StatusOK {
		t.Fatalf("enterprise pricing create status=%d body=%s", validRecorder.Code, validRecorder.Body.String())
	}

	for _, body := range []string{
		`{"name":"legacy purpose","purpose":"customer_charge","scope_type":"global","scope_id":"","model":"*","currency":"USD","authoring_mode":"raw","expression":"v1: fixed_line(\"request\", \"request\", 1)","test_cases":[]}`,
		`{"name":"legacy scope","purpose":"usage_cost","scope_type":"operator_plan","scope_id":"plan-a","model":"*","currency":"USD","authoring_mode":"raw","expression":"v1: fixed_line(\"request\", \"request\", 1)","test_cases":[]}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/costs/pricing-rules", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("legacy pricing create status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestPricingRuleHTTPVersionLifecycleAndFailureContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewService(repository, "/v1")
	router := gin.New()
	registerPricingRuleRoutes(router.Group("/costs"), service)

	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	empty := request(http.MethodGet, "/costs/pricing-rules", "")
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"data":[]`) {
		t.Fatalf("empty pricing list status=%d body=%s", empty.Code, empty.Body.String())
	}

	create := request(http.MethodPost, "/costs/pricing-rules", `{"name":"Enterprise usage","purpose":"usage_cost","scope_type":"global","scope_id":"","model":"*","currency":"USD","authoring_mode":"raw","expression":"v1: fixed_line(\"request\", \"request\", 10)","test_cases":[]}`)
	if create.Code != http.StatusOK {
		t.Fatalf("create pricing status=%d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		Data controlplane.PricingRuleDetail `json:"data"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil || created.Data.Draft == nil {
		t.Fatalf("decode create pricing: data=%+v err=%v", created.Data, err)
	}
	ruleID := created.Data.Rule.ID

	listed := request(http.MethodGet, "/costs/pricing-rules?purpose=usage_cost&status=active", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), ruleID) {
		t.Fatalf("list pricing status=%d body=%s", listed.Code, listed.Body.String())
	}
	detail := request(http.MethodGet, "/costs/pricing-rules/"+ruleID, "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), created.Data.Draft.ID) {
		t.Fatalf("get pricing rule status=%d body=%s", detail.Code, detail.Body.String())
	}
	missingDetail := request(http.MethodGet, "/costs/pricing-rules/missing", "")
	if missingDetail.Code != http.StatusNotFound {
		t.Fatalf("missing pricing rule status=%d body=%s", missingDetail.Code, missingDetail.Body.String())
	}

	valid := request(http.MethodPost, "/costs/pricing-rules/validate", `{"expression":"v1: fixed_line(\"request\", \"request\", 20)","test_cases":[]}`)
	if valid.Code != http.StatusOK || !strings.Contains(valid.Body.String(), `"valid":true`) {
		t.Fatalf("validate pricing status=%d body=%s", valid.Code, valid.Body.String())
	}
	invalid := request(http.MethodPost, "/costs/pricing-rules/validate", `{"expression":"v1: uncached_input_tokens * 3","test_cases":[]}`)
	if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), `"valid":false`) {
		t.Fatalf("invalid pricing validation status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	simulation := request(http.MethodPost, "/costs/pricing-rules/simulate", `{"expression":"v1: fixed_line(\"request\", \"request\", 20)","currency":"USD","facts":{}}`)
	if simulation.Code != http.StatusOK || !strings.Contains(simulation.Body.String(), `"amount_micros":20`) {
		t.Fatalf("simulate pricing status=%d body=%s", simulation.Code, simulation.Body.String())
	}
	missingSimulation := request(http.MethodPost, "/costs/pricing-rules/simulate", `{"rule_version_id":"missing","currency":"USD","facts":{}}`)
	if missingSimulation.Code != http.StatusNotFound {
		t.Fatalf("missing simulation version status=%d body=%s", missingSimulation.Code, missingSimulation.Body.String())
	}

	updateBody := fmt.Sprintf(`{"expected_lock_version":%d,"name":"Enterprise usage v1","currency":"USD","authoring_mode":"raw","expression":"v1: fixed_line(\"request\", \"request\", 20)","test_cases":[]}`, created.Data.Rule.LockVersion)
	updatedRec := request(http.MethodPut, "/costs/pricing-rules/"+ruleID+"/draft", updateBody)
	if updatedRec.Code != http.StatusOK {
		t.Fatalf("update pricing draft status=%d body=%s", updatedRec.Code, updatedRec.Body.String())
	}
	var updated struct {
		Data controlplane.PricingRuleDetail `json:"data"`
	}
	if err := json.Unmarshal(updatedRec.Body.Bytes(), &updated); err != nil || updated.Data.Draft == nil {
		t.Fatalf("decode updated pricing: data=%+v err=%v", updated.Data, err)
	}
	staleUpdate := request(http.MethodPut, "/costs/pricing-rules/"+ruleID+"/draft", updateBody)
	if staleUpdate.Code != http.StatusConflict {
		t.Fatalf("stale pricing draft status=%d body=%s", staleUpdate.Code, staleUpdate.Body.String())
	}

	publishBody := fmt.Sprintf(`{"draft_version_id":%q,"expected_lock_version":%d,"expected_active_version_id":"","expression_hash":%q}`, updated.Data.Draft.ID, updated.Data.Rule.LockVersion, updated.Data.Draft.ExpressionHash)
	publishedRec := request(http.MethodPost, "/costs/pricing-rules/"+ruleID+"/publish", publishBody)
	if publishedRec.Code != http.StatusOK {
		t.Fatalf("publish pricing status=%d body=%s", publishedRec.Code, publishedRec.Body.String())
	}
	var firstPublished struct {
		Data controlplane.PricingRuleDetail `json:"data"`
	}
	if err := json.Unmarshal(publishedRec.Body.Bytes(), &firstPublished); err != nil || firstPublished.Data.ActiveVersion == nil {
		t.Fatalf("decode published pricing: data=%+v err=%v", firstPublished.Data, err)
	}
	stalePublish := request(http.MethodPost, "/costs/pricing-rules/"+ruleID+"/publish", publishBody)
	if stalePublish.Code != http.StatusConflict {
		t.Fatalf("stale publish status=%d body=%s", stalePublish.Code, stalePublish.Body.String())
	}

	secondDraftBody := fmt.Sprintf(`{"expected_lock_version":%d,"name":"Enterprise usage v2","currency":"USD","authoring_mode":"raw","expression":"v1: fixed_line(\"request\", \"request\", 30)","test_cases":[]}`, firstPublished.Data.Rule.LockVersion)
	secondDraftRec := request(http.MethodPut, "/costs/pricing-rules/"+ruleID+"/draft", secondDraftBody)
	var secondDraft struct {
		Data controlplane.PricingRuleDetail `json:"data"`
	}
	if secondDraftRec.Code != http.StatusOK {
		t.Fatalf("second pricing draft status=%d body=%s", secondDraftRec.Code, secondDraftRec.Body.String())
	}
	if err := json.Unmarshal(secondDraftRec.Body.Bytes(), &secondDraft); err != nil || secondDraft.Data.Draft == nil {
		t.Fatalf("decode second draft: data=%+v err=%v", secondDraft.Data, err)
	}
	secondPublishBody := fmt.Sprintf(`{"draft_version_id":%q,"expected_lock_version":%d,"expected_active_version_id":%q,"expression_hash":%q}`, secondDraft.Data.Draft.ID, secondDraft.Data.Rule.LockVersion, firstPublished.Data.ActiveVersion.ID, secondDraft.Data.Draft.ExpressionHash)
	secondPublishedRec := request(http.MethodPost, "/costs/pricing-rules/"+ruleID+"/publish", secondPublishBody)
	var secondPublished struct {
		Data controlplane.PricingRuleDetail `json:"data"`
	}
	if secondPublishedRec.Code != http.StatusOK {
		t.Fatalf("second publish status=%d body=%s", secondPublishedRec.Code, secondPublishedRec.Body.String())
	}
	if err := json.Unmarshal(secondPublishedRec.Body.Bytes(), &secondPublished); err != nil || secondPublished.Data.ActiveVersion == nil {
		t.Fatalf("decode second publish: data=%+v err=%v", secondPublished.Data, err)
	}

	activatePath := "/costs/pricing-rules/" + ruleID + "/activate/" + firstPublished.Data.ActiveVersion.ID
	activateBody := fmt.Sprintf(`{"expected_lock_version":%d}`, secondPublished.Data.Rule.LockVersion)
	activated := request(http.MethodPost, activatePath, activateBody)
	if activated.Code != http.StatusOK || !strings.Contains(activated.Body.String(), `"status":"active"`) {
		t.Fatalf("activate pricing version status=%d body=%s", activated.Code, activated.Body.String())
	}
	staleActivate := request(http.MethodPost, activatePath, activateBody)
	if staleActivate.Code != http.StatusConflict {
		t.Fatalf("stale activate status=%d body=%s", staleActivate.Code, staleActivate.Body.String())
	}

	current := request(http.MethodGet, "/costs/pricing-rules/"+ruleID, "")
	var currentDetail struct {
		Data controlplane.PricingRuleDetail `json:"data"`
	}
	if err := json.Unmarshal(current.Body.Bytes(), &currentDetail); err != nil {
		t.Fatalf("decode current pricing detail: %v", err)
	}
	disableBody := fmt.Sprintf(`{"expected_lock_version":%d}`, currentDetail.Data.Rule.LockVersion)
	disabled := request(http.MethodPost, "/costs/pricing-rules/"+ruleID+"/disable", disableBody)
	if disabled.Code != http.StatusOK || !strings.Contains(disabled.Body.String(), `"status":"disabled"`) {
		t.Fatalf("disable pricing status=%d body=%s", disabled.Code, disabled.Body.String())
	}
	staleDisable := request(http.MethodPost, "/costs/pricing-rules/"+ruleID+"/disable", disableBody)
	if staleDisable.Code != http.StatusConflict {
		t.Fatalf("stale disable status=%d body=%s", staleDisable.Code, staleDisable.Body.String())
	}

	amount := int64(20)
	evaluation := controlplane.PricingEvaluation{
		ID: "peval_http", Purpose: controlplane.PricingPurposeUsageCost, Phase: pricing.PhaseSettlement,
		OperationID: "operation-http", AttemptID: "attempt-http", UsageVersion: 1,
		PricingRuleID: ruleID, PricingRuleVersionID: firstPublished.Data.ActiveVersion.ID,
		EngineVersion: pricing.EngineVersionV1, ExpressionHash: firstPublished.Data.ActiveVersion.ExpressionHash,
		AmountMicros: &amount, Currency: pricing.CurrencyUSD, Status: controlplane.PricingEvaluationStatusSuccess, CreatedAt: time.Now().UTC(),
	}
	if err := repository.SavePricingEvaluation(context.Background(), evaluation); err != nil {
		t.Fatal(err)
	}
	evaluationRec := request(http.MethodGet, "/costs/pricing-evaluations/"+evaluation.ID, "")
	if evaluationRec.Code != http.StatusOK || !strings.Contains(evaluationRec.Body.String(), evaluation.ID) || !strings.Contains(evaluationRec.Body.String(), `"amount_micros":20`) {
		t.Fatalf("get pricing evaluation status=%d body=%s", evaluationRec.Code, evaluationRec.Body.String())
	}
	missingEvaluation := request(http.MethodGet, "/costs/pricing-evaluations/missing", "")
	if missingEvaluation.Code != http.StatusNotFound {
		t.Fatalf("missing pricing evaluation status=%d body=%s", missingEvaluation.Code, missingEvaluation.Body.String())
	}
}
