import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Check, Download, RefreshCw, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { IdentityAuditRequestExportInput } from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  listAuditExportPermits,
  requestAuditExport,
  securityAuditExport,
} from "@/lib/capabilities";
import { downloadContent } from "@/features/governance/shared";
import { EditorDialog, ErrorBanner, ShortDate } from "./shared";

export function AuditExportRequest({
  filter,
  onClose,
}: {
  filter: IdentityAuditRequestExportInput["filter"];
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [reason, setReason] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const request = useRef<AbortController | null>(null);
  const key = useRef(crypto.randomUUID());
  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    return () => controller.abort();
  }, []);
  const mutation = useMutation({
    mutationFn: async () => {
      const signal = request.current?.signal;
      if (!signal || signal.aborted) return;
      await requestAuditExport(
        { filter, reason: reason.trim(), requestId: key.current },
        key.current,
        signal,
      );
      if (!signal.aborted) setSubmitted(true);
    },
    retry: false,
  });
  return (
    <EditorDialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onClose();
      }}
      title={t("admin:auditApproval.request")}
      description={t(
        submitted
          ? "admin:auditApproval.submitted"
          : "admin:auditApproval.requestHelp",
      )}
    >
      {!submitted && (
        <form
          className="entity-form"
          onSubmit={(event) => {
            event.preventDefault();
            if (reason.trim().length >= 5) mutation.mutate();
          }}
        >
          {mutation.error && <ErrorBanner error={mutation.error} />}
          <label className="form-field">
            <span>{t("admin:platform.reason")}</span>
            <Textarea
              required
              minLength={5}
              maxLength={500}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
          </label>
          <div className="form-actions">
            <Button
              type="button"
              variant="outline"
              disabled={mutation.isPending}
              onClick={onClose}
            >
              <X />
              {t("admin:actions.cancel")}
            </Button>
            <Button type="submit" disabled={mutation.isPending}>
              <Check />
              {t("admin:security.confirm")}
            </Button>
          </div>
        </form>
      )}
    </EditorDialog>
  );
}

export function ApprovedAuditExports({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const request = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    return () => controller.abort();
  }, []);
  const permits = useQuery({
    queryKey: [...queryKey, "approved-audit-exports"],
    queryFn: ({ signal }) => listAuditExportPermits(signal),
    retry: false,
  });
  const download = useMutation({
    mutationFn: async (requestId: string) => {
      const signal = request.current?.signal;
      if (!signal || signal.aborted) return;
      const page = await securityAuditExport({ requestId }, signal);
      if (signal.aborted) return;
      downloadContent(
        "security-audit.json",
        JSON.stringify(page.data, null, 2),
        "application/json",
      );
      await permits.refetch();
    },
    retry: false,
  });
  return (
    <section
      className="admin-panel space-y-2"
      aria-label={t("admin:auditApproval.ready")}
    >
      <div className="standalone-toolbar">
        <h2 className="font-medium">{t("admin:auditApproval.ready")}</h2>
        <Button
          type="button"
          variant="outline"
          disabled={permits.isFetching}
          onClick={() => void permits.refetch()}
        >
          <RefreshCw />
          {t("admin:actions.refresh")}
        </Button>
      </div>
      {permits.error && <ErrorBanner error={permits.error} />}
      {download.error && <ErrorBanner error={download.error} />}
      {permits.data?.data.items.length === 0 && (
        <p className="muted-text">{t("admin:auditApproval.empty")}</p>
      )}
      {permits.data?.data.items.map((permit) => (
        <div key={permit.id} className="flex flex-wrap items-center gap-3">
          <span>
            {t(
              permit.workspace
                ? "admin:security.audit.workspace"
                : "admin:security.audit.own",
            )}
          </span>
          <span>{t("admin:auditApproval.rows", { count: permit.limit })}</span>
          <ShortDate value={permit.expiresAt} />
          <Button
            type="button"
            variant="outline"
            disabled={download.isPending}
            onClick={() => download.mutate(permit.id)}
          >
            <Download />
            {t("admin:security.audit.export")}
          </Button>
        </div>
      ))}
    </section>
  );
}
