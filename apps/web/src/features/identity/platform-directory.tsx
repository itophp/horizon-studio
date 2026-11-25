import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import {
  Check,
  ChevronRight,
  CirclePause,
  LoaderCircle,
  Plus,
  ShieldOff,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type {
  IdentityPrincipalListOutput,
  IdentityTenantListOutput,
} from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  PlatformPrincipalDetail,
  principalRiskLabels,
} from "./principal-detail";
import {
  identityPlatformWorkspaces,
  identityPlatformPrincipals,
  identityPlatformProof,
  identityPlatformChange,
} from "@/lib/capabilities";
import {
  DataTable,
  EditorDialog,
  ErrorBanner,
  Loading,
  RowActions,
  ShortDate,
  type AdminColumn,
} from "./shared";

type Workspace = IdentityTenantListOutput["items"][number];
type Subject = IdentityPrincipalListOutput["items"][number];
type Change =
  | { operation: "tenant.create" }
  | {
      operation: "tenant.deactivate" | "tenant.restore" | "create";
      tenant: string;
    }
  | {
      operation: "suspend" | "disable" | "restore";
      tenant: string;
      subject: Subject;
    };

export function PlatformDirectory({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const tenant = params.get("principalTenant") ?? undefined;
  const subject = tenant ? params.get("principalId") : null;
  const setTenant = (value?: string) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      next.delete("principalId");
      if (value) next.set("principalTenant", value);
      else next.delete("principalTenant");
      return next;
    });
  const inspect = (id?: string) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      if (id) next.set("principalId", id);
      else next.delete("principalId");
      return next;
    });
  const [after, setAfter] = useState("");
  const [change, setChange] = useState<Change>();
  const workspaces = useQuery({
    queryKey: [...queryKey, "workspaces", tenant ? "" : after],
    retry: false,
    queryFn: ({ signal }) =>
      identityPlatformWorkspaces(tenant ? "" : after, signal),
  });
  const principals = useQuery({
    queryKey: [...queryKey, "principals", tenant, after],
    enabled: !!tenant && !subject,
    retry: false,
    queryFn: ({ signal }) =>
      identityPlatformPrincipals(tenant ?? "", after, signal),
  });
  const workspaceColumns: AdminColumn<Workspace>[] = [
    {
      id: "id",
      header: t("admin:registration.workspace"),
      cell: (row) => row.id,
    },
    {
      id: "status",
      header: t("admin:fields.status"),
      cell: (row) => row.status,
    },
    {
      id: "created",
      header: t("admin:review.created"),
      cell: (row) => <ShortDate value={row.createdAt} />,
    },
    {
      id: "actions",
      header: t("admin:fields.actions"),
      cell: (row) => (
        <RowActions
          actions={[
            {
              label: t("admin:platform.subjects"),
              icon: <ChevronRight />,
              onClick: () => {
                setTenant(row.id);
                setAfter("");
              },
            },
            {
              label: t(
                row.status === "active"
                  ? "admin:platform.deactivate"
                  : "admin:platform.restore",
              ),
              icon: row.status === "active" ? <CirclePause /> : <Check />,
              danger: row.status === "active",
              onClick: () =>
                setChange({
                  operation:
                    row.status === "active"
                      ? "tenant.deactivate"
                      : "tenant.restore",
                  tenant: row.id,
                }),
            },
          ]}
        />
      ),
    },
  ];
  const subjectColumns: AdminColumn<Subject>[] = [
    {
      id: "displayName",
      header: t("admin:registration.displayName"),
      cell: (row) => row.displayName || row.id,
    },
    { id: "kind", header: t("admin:platform.kind"), cell: (row) => row.kind },
    {
      id: "status",
      header: t("admin:fields.status"),
      cell: (row) => row.status,
    },
    {
      id: "source",
      header: t("admin:platform.source"),
      cell: (row) => row.source,
    },
    {
      id: "workspaces",
      header: t("admin:principalDetails.memberships"),
      cell: (row) => row.workspaceCount,
    },
    {
      id: "logins",
      header: t("admin:principalDetails.logins"),
      cell: (row) => row.loginCount,
    },
    {
      id: "risk",
      header: t("admin:principalDetails.risk"),
      cell: (row) => t(principalRiskLabels[row.riskLevel]),
    },
    {
      id: "actions",
      header: t("admin:fields.actions"),
      cell: (row) =>
        tenant && (
          <RowActions
            actions={[
              {
                label: t("admin:principalDetails.inspect"),
                icon: <ChevronRight />,
                onClick: () => inspect(row.id),
              },
              ...(row.status === "active"
                ? [
                    {
                      label: t("admin:platform.suspend"),
                      icon: <CirclePause />,
                      danger: true,
                      onClick: () =>
                        setChange({
                          operation: "suspend",
                          tenant,
                          subject: row,
                        }),
                    },
                  ]
                : []),
              ...(row.status === "inactive"
                ? []
                : [
                    {
                      label: t("admin:platform.disable"),
                      icon: <ShieldOff />,
                      danger: true,
                      onClick: () =>
                        setChange({
                          operation: "disable",
                          tenant,
                          subject: row,
                        }),
                    },
                  ]),
              ...(row.status !== "active"
                ? [
                    {
                      label: t("admin:platform.restore"),
                      icon: <Check />,
                      onClick: () =>
                        setChange({
                          operation: "restore",
                          tenant,
                          subject: row,
                        }),
                    },
                  ]
                : []),
            ]}
          />
        ),
    },
  ];
  const listing = tenant ? principals : workspaces;
  if (tenant && subject)
    return (
      <PlatformPrincipalDetail
        key={tenant + "/" + subject}
        tenant={tenant}
        id={subject}
        queryKey={queryKey}
        onBack={() => inspect()}
      />
    );
  const createLabel = tenant
    ? "admin:platform.createSubject"
    : "admin:platform.createWorkspace";
  return (
    <div className="space-y-4">
      <div className="standalone-toolbar">
        {tenant && (
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              setTenant(undefined);
              setAfter("");
            }}
          >
            <X />
            {tenant}
          </Button>
        )}
        <Button
          type="button"
          onClick={() =>
            setChange(
              tenant
                ? { operation: "create", tenant }
                : { operation: "tenant.create" },
            )
          }
        >
          <Plus />
          {t(createLabel)}
        </Button>
      </div>
      {listing.isPending ? (
        <Loading />
      ) : listing.error ? (
        <ErrorBanner
          error={listing.error}
          onRetry={() => void listing.refetch()}
        />
      ) : tenant ? (
        <DataTable
          rows={principals.data?.data.items ?? []}
          columns={subjectColumns}
          serverPage
        />
      ) : (
        <DataTable
          rows={workspaces.data?.data.items ?? []}
          columns={workspaceColumns}
          serverPage
        />
      )}
      <div className="form-actions">
        <Button
          type="button"
          variant="outline"
          disabled={!after || listing.isFetching}
          onClick={() => setAfter("")}
        >
          <X />
          {t("admin:security.workspaceMembers.first")}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={!listing.data?.data.nextAfter || listing.isFetching}
          onClick={() => setAfter(listing.data?.data.nextAfter ?? "")}
        >
          <ChevronRight />
          {t("admin:actions.next")}
        </Button>
      </div>
      {change && (
        <PlatformChangeDialog
          change={change}
          onClose={() => setChange(undefined)}
          onSaved={() => {
            setChange(undefined);
            void listing.refetch();
          }}
        />
      )}
    </div>
  );
}

function PlatformChangeDialog({
  change,
  onClose,
  onSaved,
}: {
  change: Change;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const [error, setError] = useState<unknown>();
  const [pending, setPending] = useState(false);
  const request = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    return () => controller.abort();
  }, []);
  const operation = change.operation;
  const labels = {
    "tenant.create": t("admin:platform.createWorkspace"),
    "tenant.deactivate": t("admin:platform.deactivate"),
    "tenant.restore": t("admin:platform.restore"),
    create: t("admin:platform.createSubject"),
    suspend: t("admin:platform.suspend"),
    disable: t("admin:platform.disable"),
    restore: t("admin:platform.restore"),
  };
  const create = operation === "tenant.create" || operation === "create";
  return (
    <EditorDialog
      open
      title={labels[operation]}
      description={
        "subject" in change
          ? change.subject.displayName || change.subject.id
          : "tenant" in change
            ? change.tenant
            : t("admin:platform.createWorkspaceHelp")
      }
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <form
        className="entity-form"
        onSubmit={(event) => {
          event.preventDefault();
          const signal = request.current?.signal;
          if (!signal || pending) return;
          const form = new FormData(event.currentTarget);
          const get = (key: string) => String(form.get(key) ?? "");
          const targetTenantId =
            "tenant" in change ? change.tenant : get("tenant").trim();
          const target =
            "subject" in change
              ? change.subject.id
              : create
                ? get("username").trim()
                : targetTenantId;
          const input = {
            targetTenantId,
            target,
            reason: get("reason").trim(),
          };
          if (input.reason.length < 5) return;
          setError(undefined);
          setPending(true);
          const run = async () => {
            const proof = await identityPlatformProof(
              { ...input, operation, password: get("password") },
              signal,
            );
            if (signal.aborted) return;
            await identityPlatformChange(
              operation,
              {
                ...input,
                proof: proof.data.proof,
                initialPassword: get("initialPassword"),
                displayName: get("displayName").trim(),
              },
              crypto.randomUUID(),
              signal,
            );
            if (!signal.aborted) onSaved();
          };
          void run()
            .catch((cause: unknown) => {
              if (!signal.aborted) setError(cause);
            })
            .finally(() => {
              if (!signal.aborted) setPending(false);
            });
        }}
      >
        {!!error && <ErrorBanner error={error} />}
        <fieldset className="grid gap-3" disabled={pending}>
          {operation === "tenant.create" && (
            <label className="form-field">
              <span>{t("admin:registration.workspace")}</span>
              <Input name="tenant" required minLength={2} maxLength={63} />
            </label>
          )}
          {operation === "create" && (
            <label className="form-field">
              <span>{t("admin:registration.displayName")}</span>
              <Input name="displayName" required maxLength={100} />
            </label>
          )}
          {create && (
            <>
              <label className="form-field">
                <span>{t("admin:login.username")}</span>
                <Input
                  name="username"
                  required
                  minLength={3}
                  maxLength={80}
                  autoComplete="off"
                />
              </label>
              <label className="form-field">
                <span>{t("admin:recovery.newPassword")}</span>
                <Input
                  name="initialPassword"
                  required
                  type="password"
                  minLength={12}
                  maxLength={128}
                  autoComplete="new-password"
                />
              </label>
            </>
          )}
          <label className="form-field">
            <span>{t("admin:policy.reason")}</span>
            <Textarea name="reason" required minLength={5} maxLength={500} />
          </label>
          <label className="form-field">
            <span>{t("admin:platform.password")}</span>
            <Input
              name="password"
              required
              type="password"
              maxLength={128}
              autoComplete="current-password"
            />
          </label>
        </fieldset>
        <div className="form-actions">
          <Button
            type="button"
            variant="outline"
            disabled={pending}
            onClick={onClose}
          >
            <X />
            {t("admin:actions.cancel")}
          </Button>
          <Button type="submit" disabled={pending}>
            {pending ? <LoaderCircle className="animate-spin" /> : <Check />}
            {t("admin:security.confirm")}
          </Button>
        </div>
      </form>
    </EditorDialog>
  );
}
