//go:build integration && iamperf

package tests

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
)

// This opt-in workload establishes an isolated local baseline, not a universal
// release threshold. It deliberately includes current database-backed session
// validation rather than benchmarking a precomputed Principal in memory.
func TestIAMAuthorizationBaseline(t *testing.T) {
	handler, pool, account := iamFixture(t)
	admin := loginIAM(t, handler, account.Username)
	checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
		"username": "baseline-denied", "fullName": "Baseline denied", "initialStatus": "active", "organizationId": account.OrganizationID,
		"departmentId": account.DepartmentID, "roleIds": []int64{}, "overrides": []any{}, "password": "TestOnly!2026Secure",
	}, true))
	denied := loginIAM(t, handler, "baseline-denied")
	service := identity.New(pool, false)
	trusted, err := service.Authenticate(t.Context(), admin.cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	expectedTenant := trusted.TenantID
	type quantiles struct {
		P50 float64 `json:"p50Ms"`
		P95 float64 `json:"p95Ms"`
		P99 float64 `json:"p99Ms"`
	}
	type measurement struct {
		Scenario    string    `json:"scenario"`
		Concurrency int       `json:"concurrency"`
		Samples     int       `json:"samples"`
		Seconds     float64   `json:"seconds"`
		PerSecond   float64   `json:"requestsPerSecond"`
		Session     quantiles `json:"sessionValidation"`
		Authorize   quantiles `json:"authorize"`
		Total       quantiles `json:"combined"`
	}
	percentiles := func(values []time.Duration) quantiles {
		sort.Slice(values, func(a, b int) bool { return values[a] < values[b] })
		value := func(p float64) float64 {
			return math.Round(float64(values[int(math.Ceil(p*float64(len(values))))-1])/float64(time.Microsecond)) / 1000
		}
		return quantiles{P50: value(0.5), P95: value(0.95), P99: value(0.99)}
	}
	var databaseVersion string
	if err := pool.QueryRow(t.Context(), "SHOW server_version").Scan(&databaseVersion); err != nil {
		t.Fatal(err)
	}
	results := []measurement{}
	for _, scenario := range []struct {
		name, credential string
		allowed          bool
	}{{"allowed", admin.cookie.Value, true}, {"denied", denied.cookie.Value, false}} {
		for _, concurrency := range []int{1, 8} {
			t.Run(fmt.Sprintf("%s/workers-%d", scenario.name, concurrency), func(t *testing.T) {
				const samples = 256
				sessionTimes := make([]time.Duration, samples)
				authorizeTimes := make([]time.Duration, samples)
				totals := make([]time.Duration, samples)
				invoke := func(index int) error {
					started := time.Now()
					principal, err := service.Authenticate(t.Context(), scenario.credential)
					if err != nil {
						return fmt.Errorf("session validation: %w", err)
					}
					validated := time.Now()
					decision, err := service.AuthorizeAction(t.Context(), nil, identity.AuthorizationInput{
						Principal: principal, Action: "identity.accounts.read", Permission: "accounts:read", Resource: "account", Environment: contracts.OriginHumanUI,
					})
					finished := time.Now()
					if err != nil {
						return fmt.Errorf("authorization: %w", err)
					}
					if decision.Allowed != scenario.allowed || !scenario.allowed && decision.Reason != "permission" || scenario.allowed && decision.Constraint.TenantID != expectedTenant {
						return fmt.Errorf("incorrect baseline decision: allowed=%v reason=%s", decision.Allowed, decision.Reason)
					}
					if index >= 0 {
						sessionTimes[index], authorizeTimes[index], totals[index] = validated.Sub(started), finished.Sub(validated), finished.Sub(started)
					}
					return nil
				}
				for range 8 {
					if err := invoke(-1); err != nil {
						t.Fatal(err)
					}
				}
				var next atomic.Int64
				var workers sync.WaitGroup
				failures := make(chan error, concurrency)
				started := time.Now()
				for range concurrency {
					workers.Go(func() {
						for {
							index := int(next.Add(1)) - 1
							if index >= samples {
								return
							}
							if err := invoke(index); err != nil {
								failures <- err
								return
							}
						}
					})
				}
				workers.Wait()
				elapsed := time.Since(started).Seconds()
				close(failures)
				for err := range failures {
					t.Fatal(err)
				}
				results = append(results, measurement{Scenario: scenario.name, Concurrency: concurrency, Samples: samples,
					Seconds: elapsed, PerSecond: float64(samples) / elapsed, Session: percentiles(sessionTimes), Authorize: percentiles(authorizeTimes), Total: percentiles(totals)})
			})
		}
	}
	if t.Failed() {
		return
	}
	encoded, err := json.Marshal(struct {
		At         time.Time     `json:"at"`
		PostgreSQL string        `json:"postgresql"`
		Workload   string        `json:"workload"`
		Results    []measurement `json:"results"`
	}{time.Now().UTC(), databaseVersion, "isolated fixture; 8 warmups; no decision cache; no ABAC attribute provider; excludes HTTP/Gateway/audit overhead", results})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("IAM_BASELINE %s", encoded)
}
