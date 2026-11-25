import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { ArrowDown, Download, RefreshCw, Filter } from "lucide-react";
import type {
  IdentityAuditListInput,
  IdentityAuditListOutput,
} from "@/api/generated/capability-identity";
import {
  readAuditExportPolicy,
  securityAuditExport,
  securityAuditList,
} from "@/lib/capabilities";
import {
  ApprovedAuditExports,
  AuditExportRequest,
} from "./audit-export-approval";
import { AuditExportReviewQueue } from "./audit-export-settings";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { downloadContent } from "@/features/governance/shared";
import { usePermission } from "./auth";
import {
  SecurityAuditFilters,
  type AuditFilters,
} from "./security-audit-filters";
import {
  DataTable,
  ErrorBanner,
  Loading,
  ShortDate,
  type AdminColumn,
} from "./shared";

type AuditEvent = IdentityAuditListOutput["items"][number];
const pageSize = 30;

export function SecurityAudit({ queryKey }: { queryKey: readonly unknown[] }) {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const [filters, setFilters] = useState<AuditFilters>({});
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [requestedPage, setRequestedPage] = useState<{
    context: string;
    filter: IdentityAuditListInput;
  }>();
  const canManage = usePermission("access:manage");
  const workspace = canManage && params.get("auditScope") === "workspace";
  const rawCursor = params.get("before") ?? "";
  const cursor = /^[1-9][0-9]{0,15}$/.test(rawCursor)
    ? Number(rawCursor)
    : undefined;
  const beforeId =
    cursor !== undefined && Number.isSafeInteger(cursor) ? cursor : undefined;
  const input: IdentityAuditListInput = {
    ...filters,
    beforeId,
    limit: pageSize,
    workspace,
  };
  const events = useQuery({
    queryKey: [...queryKey, "audit", input],
    queryFn: ({ signal }) => securityAuditList(input, signal),
    retry: false,
  });
  const exportPolicy = useQuery({
    queryKey: ["iam.auditExportPolicy", ...queryKey],
    queryFn: ({ signal }) => readAuditExportPolicy(signal),
    retry: false,
  });
  const pendingExports = useRef(new Set<AbortController>());
  // A context/cursor change must not complete a download from the old view.
  const context = JSON.stringify([...queryKey, input]);
  useEffect(() => {
    const pending = pendingExports.current;
    return () => {
      for (const controller of pending) controller.abort();
      pending.clear();
    };
  }, [context]);
  const exported = useMutation({
    mutationFn: async () => {
      const items = events.data?.data.items ?? [];
      if (!items.length) return;
      const controller = new AbortController();
      pendingExports.current.add(controller);
      try {
        const result = await securityAuditExport(
          {
            ...input,
            beforeId: Math.min(Number.MAX_SAFE_INTEGER, items[0].id + 1),
            limit: items.length,
          },
          controller.signal,
        );
        if (!controller.signal.aborted)
          downloadContent(
            "security-audit.json",
            JSON.stringify(result.data, null, 2),
            "application/json",
          );
      } finally {
        pendingExports.current.delete(controller);
      }
    },
    retry: false,
  });
  const rows = events.data?.data.items ?? [];
  const columns: AdminColumn<AuditEvent>[] = [
    {
      id: "at",
      header: t("admin:security.audit.at"),
      cell: (event) => <ShortDate value={event.at} />,
    },
    {
      id: "action",
      header: t("admin:security.audit.action"),
      cell: (event) => event.action,
    },
    {
      id: "outcome",
      header: t("admin:security.audit.outcome"),
      cell: (event) => event.outcome,
    },
    {
      id: "reason",
      header: t("admin:security.audit.reason"),
      cell: (event) => event.reason,
    },
    {
      id: "resourceType",
      header: t("admin:security.audit.resource"),
      cell: (event) => event.resourceType,
    },
    {
      id: "origin",
      header: t("admin:security.audit.origin"),
      cell: (event) => event.origin,
    },
    {
      id: "correlationId",
      header: t("admin:security.audit.correlation"),
      cell: (event) => (
        <span className="font-mono text-xs break-all">
          {event.correlationId}
        </span>
      ),
    },
  ];
  return (
    <div className="space-y-4">
      <p className="muted-text">{t("admin:security.audit.help")}</p>
      <div className="standalone-toolbar flex-wrap">
        {canManage && (
          <Select
            value={workspace ? "workspace" : "own"}
            onValueChange={(value) => {
              setParams((previous) => {
                const next = new URLSearchParams(previous);
                next.set(
                  "auditScope",
                  value === "workspace" ? "workspace" : "own",
                );
                next.delete("before");
                return next;
              });
            }}
          >
            <SelectTrigger aria-label={t("admin:security.audit.scope")}>
              <SelectValue>
                {workspace
                  ? t("admin:security.audit.workspace")
                  : t("admin:security.audit.own")}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="own">
                {t("admin:security.audit.own")}
              </SelectItem>
              <SelectItem value="workspace">
                {t("admin:security.audit.workspace")}
              </SelectItem>
            </SelectContent>
          </Select>
        )}
        <Button
          type="button"
          variant="outline"
          onClick={() => setFiltersOpen(true)}
        >
          <Filter />
          {t("admin:actions.filter")}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={events.isFetching}
          onClick={() => void events.refetch()}
        >
          <RefreshCw />
          {t("admin:actions.refresh")}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={
            exported.isPending ||
            events.isFetching ||
            !!events.error ||
            rows.length === 0
          }
          onClick={() => {
            if (exportPolicy.data?.data.required) {
              setRequestedPage({
                context,
                filter: {
                  ...input,
                  beforeId: Math.min(Number.MAX_SAFE_INTEGER, rows[0].id + 1),
                  afterId: rows[rows.length - 1].id - 1,
                  limit: rows.length,
                },
              });
            } else exported.mutate();
          }}
        >
          <Download />
          {t(
            exportPolicy.data?.data.required
              ? "admin:auditApproval.request"
              : "admin:security.audit.export",
          )}
        </Button>
      </div>
      {exportPolicy.data?.data.required && (
        <>
          <ApprovedAuditExports key={context} queryKey={queryKey} />
          <AuditExportReviewQueue
            key={context + "-reviews"}
            queryKey={queryKey}
          />
        </>
      )}
      {requestedPage?.context === context && (
        <AuditExportRequest
          key={context}
          filter={requestedPage.filter}
          onClose={() => setRequestedPage(undefined)}
        />
      )}
      {filtersOpen && (
        <SecurityAuditFilters
          initial={filters}
          onClose={() => setFiltersOpen(false)}
          onApply={(next) => {
            setFilters(next);
            setFiltersOpen(false);
            exported.reset();
            setParams((previous) => {
              const updated = new URLSearchParams(previous);
              updated.delete("before");
              return updated;
            });
          }}
        />
      )}
      {exported.error && !exported.isPending && (
        <ErrorBanner error={exported.error} />
      )}
      {events.isPending ? (
        <Loading />
      ) : events.error ? (
        <ErrorBanner
          error={events.error}
          onRetry={() => void events.refetch()}
        />
      ) : (
        <DataTable rows={rows} columns={columns} serverPage />
      )}
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="outline"
          disabled={beforeId === undefined || events.isFetching}
          onClick={() =>
            setParams((previous) => {
              const next = new URLSearchParams(previous);
              next.delete("before");
              return next;
            })
          }
        >
          <RefreshCw />
          {t("admin:security.audit.latest")}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={
            events.isFetching || !!events.error || rows.length < pageSize
          }
          onClick={() =>
            setParams((previous) => {
              const next = new URLSearchParams(previous);
              next.set("before", String(rows[rows.length - 1].id));
              return next;
            })
          }
        >
          <ArrowDown />
          {t("admin:security.audit.older")}
        </Button>
      </div>
    </div>
  );
}
