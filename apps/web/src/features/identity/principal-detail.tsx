import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Search, Shield } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { IdentityPrincipalDetailOutput } from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  identityPlatformProof,
  identityPrincipalDetail,
  identityPrincipalSetSecurity,
} from "@/lib/capabilities";
import { DataTable, ErrorBanner, Loading, ShortDate } from "./shared";
import { SecurityConfirm } from "./security-confirm";

const sections = [
  ["logins", "admin:principalDetails.logins"],
  ["memberships", "admin:principalDetails.memberships"],
  ["bindings", "admin:principalDetails.bindings"],
  ["roles", "admin:principalDetails.roles"],
  ["sessions", "admin:principalDetails.sessions"],
  ["auditEvents", "admin:principalDetails.auditEvents"],
] as const;

export const principalRiskLabels = {
  none: "admin:principalDetails.riskNone",
  elevated: "admin:principalDetails.riskElevated",
  high: "admin:principalDetails.riskHigh",
} as const;

export function PlatformPrincipalDetail({
  tenant,
  id,
  queryKey,
  onBack,
}: {
  tenant: string;
  id: string;
  queryKey: readonly unknown[];
  onBack: () => void;
}) {
  const { t } = useTranslation();
  const [reason, setReason] = useState("");
  const detail = useQuery({
    queryKey: [...queryKey, "principal-detail", tenant, id],
    enabled: false,
    gcTime: 0,
    retry: false,
    queryFn: ({ signal }) =>
      identityPrincipalDetail(
        { targetTenantId: tenant, principalId: id, reason },
        signal,
      ),
  });
  return (
    <div className="space-y-4">
      <div className="standalone-toolbar">
        <Button type="button" variant="outline" onClick={onBack}>
          <ArrowLeft />
          {t("admin:platform.subjects")}
        </Button>
        <span className="muted-text">{tenant}</span>
      </div>
      {detail.data ? (
        <PrincipalDetails
          item={detail.data.data}
          tenant={tenant}
          refresh={() => detail.refetch()}
        />
      ) : (
        <form
          className="entity-form admin-panel"
          onSubmit={(event) => {
            event.preventDefault();
            if (reason.trim().length >= 5) void detail.refetch();
          }}
        >
          {!!detail.error && <ErrorBanner error={detail.error} />}
          <label className="form-field">
            <span>{t("admin:platform.reason")}</span>
            <Input
              required
              minLength={5}
              maxLength={500}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
          </label>
          <Button type="submit" disabled={detail.isFetching}>
            <Search />
            {t("admin:principalDetails.inspect")}
          </Button>
        </form>
      )}
      {detail.isFetching && <Loading />}
    </div>
  );
}

function PrincipalDetails({
  item,
  tenant,
  refresh,
}: {
  item: IdentityPrincipalDetailOutput;
  tenant: string;
  refresh: () => Promise<unknown>;
}) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);
  const [riskLevel, setRisk] = useState(item.riskLevel);
  const [securityNotes, setNotes] = useState(item.securityNotes);
  const [reason, setReason] = useState("");
  return (
    <div className="space-y-4">
      <section className="admin-panel space-y-2">
        <h2 className="font-semibold">{item.displayName || item.id}</h2>
        <p className="break-all">{item.id}</p>
        <div className="flex flex-wrap gap-3">
          <span>{item.kind}</span>
          <span>{item.status}</span>
          <span>{item.source}</span>
          <ShortDate value={item.createdAt} />
        </div>
        <p>{t(principalRiskLabels[item.riskLevel])}</p>
        {item.securityNotes && (
          <p className="whitespace-pre-wrap wrap-break-word">
            {item.securityNotes}
          </p>
        )}
        <Button
          type="button"
          variant="outline"
          onClick={() => setEditing(!editing)}
        >
          <Shield />
          {t("admin:principalDetails.edit")}
        </Button>
      </section>
      <Tabs defaultValue="logins">
        <TabsList className="flex-wrap h-auto">
          {sections.map(([key, label]) => (
            <TabsTrigger key={key} value={key}>
              {t(label)}
            </TabsTrigger>
          ))}
        </TabsList>
        {sections.map(([key, label]) => (
          <TabsContent key={key} value={key}>
            <DataTable
              rows={item[key]}
              columns={[
                {
                  id: "label",
                  header: t(label),
                  cell: (row) => row.label || row.id,
                },
                {
                  id: "status",
                  header: t("admin:fields.status"),
                  cell: (row) => row.status,
                },
                {
                  id: "context",
                  header: t("admin:principalDetails.reference"),
                  cell: (row) => row.context,
                },
                {
                  id: "at",
                  header: t(
                    key === "auditEvents"
                      ? "admin:review.created"
                      : key === "logins"
                        ? "admin:fields.lastLogin"
                        : "admin:security.expires",
                  ),
                  cell: (row) => row.at && <ShortDate value={row.at} />,
                },
              ]}
            />
          </TabsContent>
        ))}
      </Tabs>
      {editing && (
        <SecurityConfirm
          title={t("admin:principalDetails.edit")}
          description={t("admin:principalDetails.editHelp")}
          showFactor
          onClose={() => setEditing(false)}
          onConfirm={async (password, key, signal, factor) => {
            const input = {
              targetTenantId: tenant,
              target: item.id,
              riskLevel,
              securityNotes,
              reason,
            };
            const proof = await identityPlatformProof(
              {
                ...input,
                operation: "setSecurity",
                password,
                factor: factor || undefined,
              },
              signal,
            );
            if (signal.aborted) return;
            await identityPrincipalSetSecurity(
              { ...input, proof: proof.data.proof },
              key,
              signal,
            );
            if (!signal.aborted) {
              setEditing(false);
              await refresh();
            }
          }}
        >
          <label className="form-field">
            <span>{t("admin:principalDetails.risk")}</span>
            <select
              className="border-input rounded-md border bg-background p-2"
              value={riskLevel}
              onChange={(event) => {
                const value = event.target.value;
                if (
                  value === "none" ||
                  value === "elevated" ||
                  value === "high"
                )
                  setRisk(value);
              }}
            >
              <option value="none">{t(principalRiskLabels.none)}</option>
              <option value="elevated">
                {t(principalRiskLabels.elevated)}
              </option>
              <option value="high">{t(principalRiskLabels.high)}</option>
            </select>
          </label>
          <label className="form-field">
            <span>{t("admin:principalDetails.notes")}</span>
            <Textarea
              value={securityNotes}
              maxLength={500}
              onChange={(event) => setNotes(event.target.value)}
            />
          </label>
          <label className="form-field">
            <span>{t("admin:platform.reason")}</span>
            <Input
              required
              minLength={5}
              maxLength={500}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
          </label>
        </SecurityConfirm>
      )}
    </div>
  );
}
