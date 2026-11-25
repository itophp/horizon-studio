import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  ArrowRight,
  Eye,
  Power,
  RefreshCw,
  Search,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { IdentityResourceListOutput } from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { listIAMResources, setIAMResourceStatus } from "@/lib/capabilities";
import {
  DataTable,
  EditorDialog,
  ErrorBanner,
  Loading,
  RowActions,
  type AdminColumn,
} from "./shared";
import { SecurityConfirm } from "./security-confirm";

type Resource = IdentityResourceListOutput["items"][number];

export function ResourceDirectory({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const [keyword, setKeyword] = useState("");
  const [search, setSearch] = useState("");
  const [after, setAfter] = useState("");
  const [details, setDetails] = useState<Resource>();
  const [change, setChange] = useState<Resource>();
  const [reason, setReason] = useState("");
  const resources = useQuery({
    queryKey: [...queryKey, search, after],
    queryFn: ({ signal }) =>
      listIAMResources(
        { after: after || undefined, search: search || undefined },
        signal,
      ),
    retry: false,
  });
  const columns: AdminColumn<Resource & { id: string }>[] = [
    {
      id: "resource",
      header: t("admin:resourceDirectory.code"),
      cell: (row) => row.resource,
    },
    {
      id: "status",
      header: t("admin:fields.status"),
      cell: (row) =>
        t(row.enabled ? "admin:status.active" : "admin:status.inactive"),
    },
    {
      id: "actions",
      header: t("admin:fields.actions"),
      cell: (row) => (
        <RowActions
          actions={[
            {
              label: t("admin:actions.details"),
              icon: <Eye />,
              onClick: () => setDetails(row),
            },
            ...(row.canDisable
              ? [
                  {
                    label: t(
                      row.enabled
                        ? "admin:resourceDirectory.disable"
                        : "admin:resourceDirectory.enable",
                    ),
                    icon: <Power />,
                    danger: row.enabled,
                    onClick: () => {
                      setChange(row);
                      setReason("");
                    },
                  },
                ]
              : []),
          ]}
        />
      ),
    },
  ];
  return (
    <div className="space-y-4">
      <form
        className="standalone-toolbar"
        onSubmit={(event) => {
          event.preventDefault();
          setSearch(keyword.trim());
          setAfter("");
        }}
      >
        <Input
          aria-label={t("admin:resourceDirectory.search")}
          value={keyword}
          maxLength={200}
          onChange={(event) => setKeyword(event.target.value)}
        />
        <Button type="submit">
          <Search />
          {t("admin:resourceDirectory.search")}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => void resources.refetch()}
        >
          <RefreshCw />
          {t("admin:actions.refresh")}
        </Button>
      </form>
      {resources.isPending ? (
        <Loading />
      ) : resources.error ? (
        <ErrorBanner error={resources.error} />
      ) : (
        <DataTable
          rows={resources.data.data.items.map((row) => ({
            ...row,
            id: row.resource,
          }))}
          columns={columns}
          serverPage
        />
      )}
      {(after || resources.data?.data.nextAfter) && (
        <div className="form-actions">
          <Button
            type="button"
            variant="outline"
            disabled={!after}
            onClick={() => setAfter("")}
          >
            <ArrowLeft />
            {t("admin:security.workspaceMembers.first")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={!resources.data?.data.nextAfter}
            onClick={() => setAfter(resources.data?.data.nextAfter ?? "")}
          >
            <ArrowRight />
            {t("admin:actions.next")}
          </Button>
        </div>
      )}
      {details && (
        <EditorDialog
          open
          onOpenChange={(open) => {
            if (!open) setDetails(undefined);
          }}
          title={details.resource}
          description={t("admin:resourceDirectory.actions")}
          wide
        >
          <ul className="max-h-[60vh] divide-y overflow-auto">
            {details.actions.map((action) => (
              <li
                key={action.name + "@" + action.version}
                className="space-y-2 py-3"
              >
                <h3 className="font-medium">
                  {[action.name, action.version].join("@")}
                </h3>
                <dl className="grid grid-cols-2 gap-x-3 text-sm">
                  <dt>{t("admin:resourceDirectory.module")}</dt>
                  <dd>{action.module}</dd>
                  <dt>{t("admin:resourceDirectory.permission")}</dt>
                  <dd>{action.permission}</dd>
                  <dt>{t("admin:resourceDirectory.origins")}</dt>
                  <dd>
                    {action.origins.length
                      ? action.origins.join(", ")
                      : t("admin:resourceDirectory.defaultOrigins")}
                  </dd>
                  <dt>{t("admin:resourceDirectory.machine")}</dt>
                  <dd>
                    {t(
                      action.machineCallable
                        ? "admin:resourceDirectory.yes"
                        : "admin:resourceDirectory.no",
                    )}
                  </dd>
                  <dt>{t("admin:resourceDirectory.attributeProvider")}</dt>
                  <dd>
                    {t(
                      action.attributeProvider
                        ? "admin:resourceDirectory.yes"
                        : "admin:resourceDirectory.no",
                    )}
                  </dd>
                  <dt>{t("admin:resourceDirectory.selector")}</dt>
                  <dd>{action.resourceSelector}</dd>
                  <dt>{t("admin:resourceDirectory.rowAttributes")}</dt>
                  <dd>{action.rowAttributes.join(", ")}</dd>
                  <dt>{t("admin:resourceDirectory.maskedFields")}</dt>
                  <dd>{action.maskedFields.join(", ")}</dd>
                </dl>
              </li>
            ))}
          </ul>
        </EditorDialog>
      )}
      {change && (
        <SecurityConfirm
          title={t(
            change.enabled
              ? "admin:resourceDirectory.disable"
              : "admin:resourceDirectory.enable",
          )}
          description={t("admin:resourceDirectory.changeHelp", {
            resource: change.resource,
          })}
          showFactor
          onClose={() => setChange(undefined)}
          onConfirm={async (password, key, signal, factor) => {
            await setIAMResourceStatus(
              {
                resource: change.resource,
                enabled: !change.enabled,
                reason,
                password,
                factor: factor || undefined,
              },
              key,
              signal,
            );
            if (!signal.aborted) await resources.refetch();
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
    </div>
  );
}
