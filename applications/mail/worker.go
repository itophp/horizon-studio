package mail

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
	"horizon/internal/repository/dbgen/maildb"
)

func Run(ctx context.Context, pool *pgxpool.Pool, gw *gateway.Gateway) {
	if pool == nil || gw == nil {
		return
	}
	runOnce(ctx, pool, gw)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce(ctx, pool, gw)
		}
	}
}

func runOnce(ctx context.Context, pool *pgxpool.Pool, gw *gateway.Gateway) {
	q := dbgen.New(pool)
	receive := maildb.New(pool)
	tenants, err := q.MailTenants(ctx)
	if err != nil {
		slog.Error("mail worker tenant discovery failed", "code", "unavailable")
		return
	}
	for _, tenant := range tenants {
		if ctx.Err() != nil {
			return
		}
		principal, err := identity.NewPrincipal(&genidentity.AccountRecord{
			ID: 0, DepartmentID: 0, Status: "active",
		}, tenant, []*genidentity.PermissionGrant{{Permission: "mail:sync", Effect: "allow", Scope: "ALL"}})
		if err != nil {
			slog.Error("mail worker identity failed", "code", "internal")
			continue
		}
		workerCtx := identity.WithPrincipal(ctx, principal)
		channels, err := receive.ReceiveDueIDs(workerCtx, tenant)
		if err != nil {
			slog.Error("mail receive discovery failed", "code", "unavailable")
			continue
		}
		for _, channel := range channels {
			capability := "mail.inbox.sync"
			if channel.Protocol == "imap" {
				capability = "mail.inbox.imap.sync"
			}
			invokeMail(workerCtx, gw, capability, "mail-sync:"+tenant, contracts.Input{"channelId": channel.ID})
		}
		deliveries, err := q.MailOutboundDueIDs(ctx, dbgen.MailOutboundDueIDsParams{TenantID: tenant, Limit: 20})
		if err != nil {
			slog.Error("mail delivery discovery failed", "code", "unavailable")
			continue
		}
		for _, deliveryID := range deliveries {
			invokeMail(workerCtx, gw, "mail.outbound.process", "mail-outbound:"+tenant, contracts.Input{"id": deliveryID})
		}
	}
}

func invokeMail(ctx context.Context, gw *gateway.Gateway, name, prefix string, input contracts.Input) {
	key, err := governance.Token()
	if err != nil {
		return
	}
	result, err := gw.Invoke(ctx, gateway.Request{
		Name: name, Version: version, Input: input, IdempotencyKey: prefix + ":" + key,
		Origin: contracts.OriginEvent,
	})
	if err != nil {
		slog.Error("mail invocation failed", "capability", name, "code", contracts.Code(err))
		return
	}
	if result.Status != contracts.StatusSucceeded {
		slog.Error("mail execution failed", "capability", name, "status", result.Status, "code", result.ErrorCode)
	}
}
