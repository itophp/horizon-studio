import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import {
  Eye,
  Plus,
  RefreshCw,
  ShieldCheck,
  ShieldAlert,
  ClipboardCheck,
  Building2,
  Link2,
} from "lucide-react";
import { useState } from "react";
import type { IdentityPolicyHistoryOutput } from "@/api/generated/capability-identity";
import { identityPolicyHistory } from "@/lib/capabilities";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import type { ComponentRendererProps } from "@/features/application-runtime/protocol/component-registry";
import { usePermission, useSession } from "./auth";
import {
  DataTable,
  ErrorBanner,
  Loading,
  RowActions,
  type AdminColumn,
} from "./shared";
import { PolicyDetail } from "./policy-detail";
import { PolicyDraftDialog } from "./policy-draft";
import { PlatformDirectory } from "./platform-directory";
import { AccessReviews } from "./access-reviews";
import { IdentityBindings } from "./identity-bindings";
import { SecurityRiskReport } from "./security-risk-report";
import { ResourceDirectory } from "./resource-directory";
import {
  AuditExportReviewQueue,
  AuditExportSettings,
} from "./audit-export-settings";
import { ListChecks, ScrollText } from "lucide-react";

type Version = IdentityPolicyHistoryOutput["items"][number];

export function PolicyPanel({ readOnly }: ComponentRendererProps) {
  const { t } = useTranslation();
  const allowed = usePermission("access:manage");
  const session = useSession();
  if (readOnly) return <p>{t("runtime:design.previewHint")}</p>;
  if (!allowed || session.identityBindingId)
    return <p>{t("admin:errors.forbidden")}</p>;
  return <PolicyWorkspace />;
}

function PolicyWorkspace() {
  const { t } = useTranslation();
  const session = useSession();
  const [params, setParams] = useSearchParams();
  const context = JSON.stringify([
    session.tenantId,
    session.account.id,
    session.identityBindingId,
    session.identityBindingId,
    session.authorizationVersion,
  ]);
  const requestedTab = params.get("tab");
  const tab =
    requestedTab === "risks" ||
    requestedTab === "reviews" ||
    requestedTab === "bindings" ||
    requestedTab === "resources" ||
    requestedTab === "audit-export" ||
    (requestedTab === "platform" && session.platformOperator)
      ? requestedTab
      : "policies";
  return (
    <Tabs
      key={context}
      value={tab}
      onValueChange={(value) =>
        setParams((previous) => {
          const next = new URLSearchParams(previous);
          next.set("tab", String(value));
          return next;
        })
      }
    >
      <TabsList>
        <TabsTrigger value="policies">
          <ShieldCheck />
          {t("admin:policy.policiesTab")}
        </TabsTrigger>
        <TabsTrigger value="risks">
          <ShieldAlert />
          {t("admin:risk.title")}
        </TabsTrigger>
        <TabsTrigger value="reviews">
          <ClipboardCheck />
          {t("admin:review.title")}
        </TabsTrigger>
        <TabsTrigger value="bindings">
          <Link2 />
          {t("entity:identity_binding.label")}
        </TabsTrigger>
        <TabsTrigger value="resources">
          <ListChecks />
          {t("admin:resourceDirectory.title")}
        </TabsTrigger>
        <TabsTrigger value="audit-export">
          <ScrollText />
          {t("admin:auditApproval.settings")}
        </TabsTrigger>
        {session.platformOperator && (
          <TabsTrigger value="platform">
            <Building2 />
            {t("admin:platform.title")}
          </TabsTrigger>
        )}
      </TabsList>
      <TabsContent value="policies">
        {tab === "policies" && (
          <PolicyVersions
            queryKey={["iam.policy", context]}
            params={params}
            setParams={setParams}
          />
        )}
      </TabsContent>
      <TabsContent value="risks">
        {tab === "risks" && (
          <SecurityRiskReport queryKey={["iam.risks", context]} />
        )}
      </TabsContent>
      <TabsContent value="reviews">
        {tab === "reviews" && (
          <AccessReviews queryKey={["iam.reviews", context]} />
        )}
      </TabsContent>
      <TabsContent value="bindings">
        {tab === "bindings" && (
          <IdentityBindings queryKey={["iam.bindings", context]} />
        )}
      </TabsContent>
      <TabsContent value="resources">
        {tab === "resources" && (
          <ResourceDirectory queryKey={["iam.resources", context]} />
        )}
      </TabsContent>
      <TabsContent value="audit-export">
        {tab === "audit-export" && (
          <>
            <AuditExportSettings
              queryKey={["iam.auditExportPolicy", context]}
            />
            <AuditExportReviewQueue queryKey={["iam.auditReview", context]} />
          </>
        )}
      </TabsContent>
      {session.platformOperator && (
        <TabsContent value="platform">
          {tab === "platform" && (
            <PlatformDirectory queryKey={["iam.platform", context]} />
          )}
        </TabsContent>
      )}
    </Tabs>
  );
}

function PolicyVersions({
  queryKey,
  params,
  setParams,
}: {
  queryKey: readonly unknown[];
  params: URLSearchParams;
  setParams: ReturnType<typeof useSearchParams>[1];
}) {
  const { t } = useTranslation();
  const [draftOpen, setDraftOpen] = useState(false);
  const history = useQuery({
    queryKey: [...queryKey, "history"],
    queryFn: ({ signal }) => identityPolicyHistory(signal),
    retry: false,
  });
  const raw = params.get("policy") ?? "";
  const selected =
    /^[1-9][0-9]*$/.test(raw) && Number.isSafeInteger(Number(raw))
      ? Number(raw)
      : undefined;
  const navigate = (id?: number) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      if (id) next.set("policy", String(id));
      else next.delete("policy");
      return next;
    });
  if (selected)
    return (
      <PolicyDetail
        key={selected}
        id={selected}
        queryKey={queryKey}
        onBack={() => navigate()}
        onChanged={() => {
          void history.refetch();
        }}
      />
    );
  if (history.isPending) return <Loading />;
  if (history.error)
    return (
      <ErrorBanner
        error={history.error}
        onRetry={() => void history.refetch()}
      />
    );
  const current = history.data.data.version;
  const columns: AdminColumn<Version>[] = [
    {
      id: "reason",
      header: t("admin:policy.reason"),
      cell: (row) => row.reason,
    },
    {
      id: "version",
      header: t("admin:policy.version"),
      cell: (row) => t("admin:policy.versionLabel", { version: row.id }),
    },
    {
      id: "state",
      header: t("admin:fields.status"),
      cell: (row) =>
        t(
          row.id === current
            ? "admin:policy.current"
            : row.published
              ? "admin:policy.published"
              : "admin:policy.draft",
        ),
    },
    {
      id: "author",
      header: t("admin:policy.author"),
      cell: (row) =>
        t(row.authoredByMe ? "admin:policy.you" : "admin:policy.anotherAdmin"),
    },
    {
      id: "actions",
      header: t("admin:fields.actions"),
      cell: (row) => (
        <RowActions
          actions={[
            {
              label: t("admin:policy.review"),
              icon: <Eye />,
              onClick: () => navigate(row.id),
            },
          ]}
        />
      ),
    },
  ];
  return (
    <div className="space-y-4">
      <p className="muted-text">
        {t("admin:policy.historyHelp", { version: current })}
      </p>
      <div className="standalone-toolbar flex-wrap">
        <Button type="button" onClick={() => setDraftOpen(true)}>
          <Plus />
          {t("admin:policy.create")}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={history.isFetching}
          onClick={() => void history.refetch()}
        >
          <RefreshCw />
          {t("admin:actions.refresh")}
        </Button>
      </div>
      <DataTable rows={history.data.data.items} columns={columns} serverPage />
      {draftOpen && (
        <PolicyDraftDialog
          baseVersion={current}
          queryKey={queryKey}
          onClose={() => setDraftOpen(false)}
          onSaved={(id) => {
            setDraftOpen(false);
            void history.refetch();
            navigate(id);
          }}
        />
      )}
    </div>
  );
}
