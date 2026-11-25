import {
  capabilityApprovals,
  capabilityDecide,
} from "@/api/generated/baseline";
import { ApiError } from "@/api/client/request";
import { usePermission } from "./auth";
import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Check, RefreshCw, ShieldCheck, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  configureAuditExport,
  readAuditExportPolicy,
} from "@/lib/capabilities";
import { SecurityConfirm } from "./security-confirm";
import { EditorDialog, ErrorBanner, Loading, ShortDate } from "./shared";

export function AuditExportSettings({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const policy = useQuery({
    queryKey,
    queryFn: ({ signal }) => readAuditExportPolicy(signal),
    retry: false,
  });
  if (policy.isPending) return <Loading />;
  if (policy.error)
    return (
      <ErrorBanner error={policy.error} onRetry={() => void policy.refetch()} />
    );
  const required = policy.data.data.required;
  return (
    <section className="admin-panel space-y-3">
      <p>
        {t(
          required
            ? "admin:auditApproval.required"
            : "admin:auditApproval.optional",
        )}
      </p>
      <Button
        type="button"
        variant="outline"
        onClick={() => {
          setReason("");
          setOpen(true);
        }}
      >
        <ShieldCheck />
        {t(
          required
            ? "admin:auditApproval.turnOff"
            : "admin:auditApproval.turnOn",
        )}
      </Button>
      {open && (
        <SecurityConfirm
          title={t("admin:auditApproval.settings")}
          description={t("admin:auditApproval.settingsHelp")}
          showFactor
          onClose={() => setOpen(false)}
          onConfirm={async (password, key, signal, factor) => {
            await configureAuditExport(
              {
                required: !required,
                password,
                factor: factor || undefined,
                reason,
              },
              key,
              signal,
            );
            if (!signal.aborted) await policy.refetch();
          }}
        >
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
    </section>
  );
}

export function AuditExportReviewQueue({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const permitted = usePermission("capability:approve");
  const [selected, setSelected] = useState<{ id: number; approve: boolean }>();
  const [error, setError] = useState<unknown>();
  const queue = useQuery({
    queryKey: [...queryKey, "audit-export-review"],
    enabled: permitted,
    retry: false,
    queryFn: async ({ signal }) => {
      const response = await capabilityApprovals({ signal });
      if (response.status !== 200)
        throw new ApiError("forbidden", response.status);
      return response.data.filter(
        (item) =>
          item.capability === "identity.audit.requestExport" && !item.mine,
      );
    },
  });
  const decision = useMutation({
    retry: false,
    mutationFn: async ({ id, approve }: { id: number; approve: boolean }) => {
      const response = await capabilityDecide(id, { approve });
      if (response.status !== 200)
        throw new ApiError("forbidden", response.status);
      if (approve && response.data.status !== "succeeded")
        throw new ApiError(response.data.errorCode || "internal", 400);
    },
    onSuccess: async () => {
      setSelected(undefined);
      await queue.refetch();
    },
    onError: (cause) => setError(cause),
  });
  if (!permitted) return null;
  if (queue.isPending) return <Loading />;
  if (queue.error)
    return (
      <ErrorBanner error={queue.error} onRetry={() => void queue.refetch()} />
    );
  return (
    <section className="admin-panel space-y-3">
      <div className="standalone-toolbar">
        <h2 className="font-medium">{t("admin:auditApproval.review")}</h2>
        <Button
          type="button"
          variant="outline"
          onClick={() => void queue.refetch()}
        >
          <RefreshCw />
          {t("admin:actions.refresh")}
        </Button>
      </div>
      {queue.data?.length === 0 && (
        <p className="muted-text">{t("admin:auditApproval.noRequests")}</p>
      )}
      {queue.data?.map((card) => {
        const args = card.arguments as
          { reason?: string; filter?: Record<string, unknown> } | undefined;
        return (
          <article key={card.id} className="space-y-2 rounded-md border p-3">
            <p className="break-words">{args?.reason}</p>
            <ShortDate value={card.expiresAt} />
            {args?.filter && (
              <pre className="max-h-40 overflow-auto whitespace-pre-wrap text-xs">
                {JSON.stringify(args.filter, null, 2)}
              </pre>
            )}
            <div className="form-actions">
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  setError(undefined);
                  setSelected({ id: card.id, approve: false });
                }}
              >
                <X />
                {t("admin:auditApproval.reject")}
              </Button>
              <Button
                type="button"
                onClick={() => {
                  setError(undefined);
                  setSelected({ id: card.id, approve: true });
                }}
              >
                <Check />
                {t("admin:auditApproval.approve")}
              </Button>
            </div>
          </article>
        );
      })}
      {selected && (
        <EditorDialog
          open
          onOpenChange={(open) => {
            if (!open && !decision.isPending) setSelected(undefined);
          }}
          title={t(
            selected.approve
              ? "admin:auditApproval.approve"
              : "admin:auditApproval.reject",
          )}
          description={t("admin:auditApproval.decideHelp")}
        >
          {!!error && <ErrorBanner error={error} />}
          <div className="form-actions">
            <Button
              type="button"
              variant="outline"
              disabled={decision.isPending}
              onClick={() => setSelected(undefined)}
            >
              <X />
              {t("admin:actions.cancel")}
            </Button>
            <Button
              type="button"
              disabled={decision.isPending}
              onClick={() => decision.mutate(selected)}
            >
              <Check />
              {t("admin:security.confirm")}
            </Button>
          </div>
        </EditorDialog>
      )}
    </section>
  );
}
