package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strconv"

	canonical "horizon/foundation/kernel/memory"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
)

// observeDenied records an authorization decision without input, credentials,
// error text or a fabricated invocation ID. It never affects the decision.
func (g *Gateway) observeDenied(ctx context.Context, p *identity.Principal, request Request, capability, reason string) {
	if g.pool != nil {
		authority, _ := json.Marshal(map[string]string{
			"origin": string(request.Origin), "machineCredentialId": p.MachineCredentialID(), "delegationId": p.AgentDelegationID(),
		})
		if err := dbgen.New(g.pool).SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
			TenantID: p.TenantID, AccountID: p.Account.ID, Binding: p.IdentityBindingID,
			Action: capability, Outcome: "denied", Reason: reason, CorrelationID: request.CorrelationID, Authority: authority,
		}); err != nil {
			slog.Error("security audit append failed", "action", capability)
		}
	}
	if g.observations == nil {
		return
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		slog.Warn("memory observation dropped", "code", "id_unavailable")
		return
	}
	g.observations.Submit(canonical.Envelope{EventID: "denied:" + hex.EncodeToString(raw[:]), EventType: "capability.invocation.denied",
		TenantID: p.TenantID, Actor: canonical.Actor{ID: strconv.FormatInt(p.Account.ID, 10), Type: "user"},
		OccurredAt: g.now(), Source: canonical.Source{Type: "capability", ID: capability}, Result: "denied", CorrelationID: request.CorrelationID,
		CausationID: request.CausationID, Depth: request.TriggerDepth, Sensitivity: "internal", Payload: map[string]string{"reason": reason}})
}
