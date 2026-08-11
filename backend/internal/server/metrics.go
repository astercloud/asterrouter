package server

import (
	"context"
	"crypto/subtle"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/gin-gonic/gin"
)

type serverMetricKey struct {
	method string
	route  string
	status int
}

type serverMetricValue struct {
	count         uint64
	durationNanos uint64
}

type capacityAdmissionMetricKey struct {
	scope  string
	result string
	reason string
}

type providerCapacitySnapshotSource func(context.Context) ([]controlplane.ProviderCapacityMetricSnapshot, error)

type serverMetrics struct {
	mu                    sync.Mutex
	http                  map[serverMetricKey]serverMetricValue
	inFlight              int64
	readinessStatus       bool
	readinessReadyChecks  uint64
	readinessFailedChecks uint64
	capacityAdmissions    map[capacityAdmissionMetricKey]uint64
	providerCapacity      providerCapacitySnapshotSource
}

func newServerMetrics() *serverMetrics {
	return &serverMetrics{
		http:               make(map[serverMetricKey]serverMetricValue),
		capacityAdmissions: make(map[capacityAdmissionMetricKey]uint64),
	}
}

func (m *serverMetrics) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}
		startedAt := time.Now()
		m.mu.Lock()
		m.inFlight++
		m.mu.Unlock()
		defer func() {
			route := c.FullPath()
			if route == "" {
				route = "unmatched"
			}
			key := serverMetricKey{method: prometheusHTTPMethod(c.Request.Method), route: route, status: c.Writer.Status()}
			duration := uint64(time.Since(startedAt))
			m.mu.Lock()
			value := m.http[key]
			value.count++
			value.durationNanos += duration
			m.http[key] = value
			m.inFlight--
			m.mu.Unlock()
		}()
		c.Next()
	}
}

func (m *serverMetrics) recordReadiness(ready bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readinessStatus = ready
	if ready {
		m.readinessReadyChecks++
		return
	}
	m.readinessFailedChecks++
}

func (m *serverMetrics) ObserveCapacityAdmission(event controlplane.CapacityAdmissionEvent) {
	key := capacityAdmissionMetricKey{
		scope: prometheusCapacityScope(event.Scope), result: prometheusCapacityResult(event.Result), reason: prometheusCapacityReason(event.Reason),
	}
	if key.result == "error" && key.reason == "none" {
		key.reason = "other"
	}
	m.mu.Lock()
	m.capacityAdmissions[key]++
	m.mu.Unlock()
}

func (m *serverMetrics) setProviderCapacitySnapshotSource(source providerCapacitySnapshotSource) {
	m.mu.Lock()
	m.providerCapacity = source
	m.mu.Unlock()
}

func (m *serverMetrics) handle(c *gin.Context) {
	c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", []byte(m.renderContext(c.Request.Context())))
}

func (m *serverMetrics) render() string {
	return m.renderContext(context.Background())
}

func (m *serverMetrics) renderContext(ctx context.Context) string {
	m.mu.Lock()
	values := make(map[serverMetricKey]serverMetricValue, len(m.http))
	for key, value := range m.http {
		values[key] = value
	}
	inFlight := m.inFlight
	readinessStatus := m.readinessStatus
	readyChecks := m.readinessReadyChecks
	failedChecks := m.readinessFailedChecks
	capacityAdmissions := make(map[capacityAdmissionMetricKey]uint64, len(m.capacityAdmissions))
	for key, value := range m.capacityAdmissions {
		capacityAdmissions[key] = value
	}
	providerCapacity := m.providerCapacity
	m.mu.Unlock()

	var providerSnapshots []controlplane.ProviderCapacityMetricSnapshot
	providerSnapshotStatus := -1
	if providerCapacity != nil {
		providerSnapshotStatus = 1
		var err error
		providerSnapshots, err = providerCapacity(ctx)
		if err != nil {
			providerSnapshotStatus = 0
			providerSnapshots = nil
		}
	}

	keys := make([]serverMetricKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := keys[i], keys[j]
		if left.route != right.route {
			return left.route < right.route
		}
		if left.method != right.method {
			return left.method < right.method
		}
		return left.status < right.status
	})

	var output strings.Builder
	output.WriteString("# HELP asterrouter_http_requests_total Completed HTTP requests.\n")
	output.WriteString("# TYPE asterrouter_http_requests_total counter\n")
	for _, key := range keys {
		value := values[key]
		labels := prometheusHTTPLabels(key)
		output.WriteString("asterrouter_http_requests_total" + labels + " " + strconv.FormatUint(value.count, 10) + "\n")
	}
	output.WriteString("# HELP asterrouter_http_request_duration_seconds HTTP request duration.\n")
	output.WriteString("# TYPE asterrouter_http_request_duration_seconds summary\n")
	for _, key := range keys {
		value := values[key]
		labels := prometheusHTTPLabels(key)
		seconds := float64(value.durationNanos) / float64(time.Second)
		output.WriteString("asterrouter_http_request_duration_seconds_sum" + labels + " " + strconv.FormatFloat(seconds, 'g', -1, 64) + "\n")
		output.WriteString("asterrouter_http_request_duration_seconds_count" + labels + " " + strconv.FormatUint(value.count, 10) + "\n")
	}
	output.WriteString("# HELP asterrouter_http_requests_in_flight Current HTTP requests excluding the metrics scrape.\n")
	output.WriteString("# TYPE asterrouter_http_requests_in_flight gauge\n")
	output.WriteString("asterrouter_http_requests_in_flight " + strconv.FormatInt(inFlight, 10) + "\n")
	output.WriteString("# HELP asterrouter_readiness_status Last readiness result.\n")
	output.WriteString("# TYPE asterrouter_readiness_status gauge\n")
	if readinessStatus {
		output.WriteString("asterrouter_readiness_status 1\n")
	} else {
		output.WriteString("asterrouter_readiness_status 0\n")
	}
	output.WriteString("# HELP asterrouter_readiness_checks_total Readiness checks by result.\n")
	output.WriteString("# TYPE asterrouter_readiness_checks_total counter\n")
	output.WriteString("asterrouter_readiness_checks_total{result=\"ready\"} " + strconv.FormatUint(readyChecks, 10) + "\n")
	output.WriteString("asterrouter_readiness_checks_total{result=\"unavailable\"} " + strconv.FormatUint(failedChecks, 10) + "\n")
	writeCapacityAdmissionMetrics(&output, capacityAdmissions)
	if providerSnapshotStatus >= 0 {
		writeProviderCapacityMetrics(&output, providerSnapshots, providerSnapshotStatus)
	}
	return output.String()
}

func writeCapacityAdmissionMetrics(output *strings.Builder, values map[capacityAdmissionMetricKey]uint64) {
	output.WriteString("# HELP asterrouter_capacity_admissions_total Capacity admission decisions by bounded scope, result, and reason.\n")
	output.WriteString("# TYPE asterrouter_capacity_admissions_total counter\n")
	keys := make([]capacityAdmissionMetricKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].scope != keys[j].scope {
			return keys[i].scope < keys[j].scope
		}
		if keys[i].result != keys[j].result {
			return keys[i].result < keys[j].result
		}
		return keys[i].reason < keys[j].reason
	})
	for _, key := range keys {
		labels := `{scope="` + key.scope + `",result="` + key.result + `",reason="` + key.reason + `"}`
		output.WriteString("asterrouter_capacity_admissions_total" + labels + " " + strconv.FormatUint(values[key], 10) + "\n")
	}
}

func writeProviderCapacityMetrics(output *strings.Builder, snapshots []controlplane.ProviderCapacityMetricSnapshot, snapshotStatus int) {
	output.WriteString("# HELP asterrouter_provider_capacity_snapshot_status Whether the current provider capacity snapshot succeeded.\n")
	output.WriteString("# TYPE asterrouter_provider_capacity_snapshot_status gauge\n")
	output.WriteString("asterrouter_provider_capacity_snapshot_status " + strconv.Itoa(snapshotStatus) + "\n")
	output.WriteString("# HELP asterrouter_provider_capacity_current Current provider capacity consumption by dimension.\n")
	output.WriteString("# TYPE asterrouter_provider_capacity_current gauge\n")
	output.WriteString("# HELP asterrouter_provider_capacity_limit Configured provider capacity limit by dimension; zero disables the limit.\n")
	output.WriteString("# TYPE asterrouter_provider_capacity_limit gauge\n")
	output.WriteString("# HELP asterrouter_provider_account_schedulable Whether the provider account is active and schedulable.\n")
	output.WriteString("# TYPE asterrouter_provider_account_schedulable gauge\n")
	output.WriteString("# HELP asterrouter_provider_account_circuit_open Whether the provider account circuit is open.\n")
	output.WriteString("# TYPE asterrouter_provider_account_circuit_open gauge\n")
	for _, snapshot := range snapshots {
		baseLabels := prometheusProviderLabels(snapshot.ProviderID, snapshot.ProviderAccountID)
		output.WriteString("asterrouter_provider_account_schedulable" + baseLabels + " " + prometheusBool(snapshot.Schedulable) + "\n")
		output.WriteString("asterrouter_provider_account_circuit_open" + baseLabels + " " + prometheusBool(snapshot.CircuitOpen) + "\n")
		for _, dimension := range []struct {
			name    string
			current int
			limit   int
		}{
			{name: "concurrency", current: snapshot.Current.CapacityUnits, limit: snapshot.ConcurrencyLimit},
			{name: "rpm", current: snapshot.Current.Requests, limit: snapshot.RPMLimit},
			{name: "tpm", current: snapshot.Current.Tokens, limit: snapshot.TPMLimit},
		} {
			labels := prometheusProviderDimensionLabels(snapshot.ProviderID, snapshot.ProviderAccountID, dimension.name)
			output.WriteString("asterrouter_provider_capacity_current" + labels + " " + strconv.Itoa(dimension.current) + "\n")
			output.WriteString("asterrouter_provider_capacity_limit" + labels + " " + strconv.Itoa(dimension.limit) + "\n")
		}
	}
}

func prometheusProviderLabels(providerID, providerAccountID string) string {
	return `{provider="` + prometheusLabelValue(providerID) + `",provider_account="` + prometheusLabelValue(providerAccountID) + `"}`
}

func prometheusProviderDimensionLabels(providerID, providerAccountID, dimension string) string {
	return `{provider="` + prometheusLabelValue(providerID) + `",provider_account="` + prometheusLabelValue(providerAccountID) + `",dimension="` + dimension + `"}`
}

func prometheusBool(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func prometheusCapacityScope(scope string) string {
	switch strings.TrimSpace(scope) {
	case "credential", "application", "provider_account":
		return strings.TrimSpace(scope)
	default:
		return "other"
	}
}

func prometheusCapacityResult(result string) string {
	switch strings.TrimSpace(result) {
	case "acquired", "rejected", "error":
		return strings.TrimSpace(result)
	default:
		return "other"
	}
}

func prometheusCapacityReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "":
		return "none"
	case "concurrency_exhausted", "at_capacity":
		return "concurrency"
	case "application_concurrency_exhausted":
		return "application_concurrency"
	case "qps_exhausted":
		return "qps"
	case "rpm_exhausted":
		return "rpm"
	case "tpm_exhausted":
		return "tpm"
	case "circuit_open":
		return "circuit_open"
	case "circuit_half_open_busy":
		return "circuit_half_open"
	case "capacity_store_unavailable":
		return "store_unavailable"
	case "provider_account_missing", "credential_missing":
		return "invalid"
	default:
		return "other"
	}
}

func prometheusHTTPLabels(key serverMetricKey) string {
	return `{method="` + prometheusLabelValue(key.method) + `",route="` + prometheusLabelValue(key.route) + `",status="` + strconv.Itoa(key.status) + `"}`
}

func prometheusHTTPMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	default:
		return "OTHER"
	}
}

func prometheusLabelValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func requireMetricsToken(expected string) gin.HandlerFunc {
	expected = strings.TrimSpace(expected)
	return func(c *gin.Context) {
		if expected == "" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		provided := bearerToken(c)
		if provided == "" {
			provided = strings.TrimSpace(c.GetHeader("X-Metrics-Token"))
		}
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}
