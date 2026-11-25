import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Braces, Check, Copy, GitBranch, Plus, Puzzle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { ApiError } from "@/api/client/request";
import { Button } from "@/components/ui/button";
import { z } from "zod";
import { useSession } from "@/features/identity/auth";
import { ErrorBanner, Loading } from "@/features/identity/shared";
import { studio, type CatalogEntry, type CatalogKind, type Item } from "./api";
import {
  DetailField,
  Library,
  LibraryEmpty,
  RecordHeader,
  type Selection,
} from "./library";

import { PluginManifestFields } from "./plugins-library";
import { CatalogPlanDialog } from "./catalog-plan-dialog";
import type { TaskDraft } from "./new-task";
import { studioWorkbenchPath } from "./routes";

const workflowStepsSchema = z.array(
  z.object({
    id: z.string(),
    capability: z.string().optional(),
    version: z.string().optional(),
  }),
);

const referenceOf = (row: CatalogEntry) => `${row.id}@${row.version}`;

const catalogAddLabel = {
  capability: "planCapability",
  workflow: "planWorkflow",
  plugin: "planPlugin",
} as const;

export function CatalogLibrary({
  project,
  kind,
  selected,
  onSelect,
  items,
  canWrite,
  refresh,
  onCreateTask,
}: {
  project: number;
  kind: CatalogKind;
  selected?: string;
  onSelect: Selection;
  items: Item[];
  canWrite: boolean;
  refresh: () => Promise<void>;
  onCreateTask: (draft: TaskDraft) => void;
}) {
  const { t } = useTranslation("studio");
  const profile = useSession();
  const [planning, setPlanning] = useState(false);
  const planned = items.filter(
    (item) => item.kind === kind && item.catalogTarget,
  );
  const scope = [
    profile.tenantId,
    profile.account.id,
    profile.identityBindingId,
    profile.authorizationVersion,
  ];
  const listing = useQuery({
    queryKey: ["studio-catalog", ...scope, project, kind],
    queryFn: ({ signal }) => studio.catalog(project, kind, signal),
    refetchInterval: (query) =>
      planned.some(
        (plan) =>
          items.some((task) => task.subjectItemId === plan.id) &&
          !query.state.data?.some(
            (entry) => referenceOf(entry) === plan.catalogTarget,
          ),
      )
        ? 10000
        : false,
  });
  const declarations = useQuery({
    queryKey: ["studio-project-plugins", ...scope, project],
    queryFn: ({ signal }) => studio.projectPlugins(project, signal),
    enabled: kind === "plugin",
  });
  const manifest = declarations.data?.find(
    (entry) => referenceOf(entry) === selected,
  );
  const row = listing.data?.find((item) => referenceOf(item) === selected);
  const plan = planned.find((item) => item.catalogTarget === selected);
  const relatedTasks = items.filter(
    (item) => plan && item.subjectItemId === plan.id,
  );
  const catalogStatus = (item: Item) =>
    listing.data?.some((entry) => referenceOf(entry) === item.catalogTarget)
      ? t("catalogCompleted")
      : items.some((task) => task.subjectItemId === item.id)
        ? t("catalogDeveloping")
        : t("catalogPlanned");
  const definition = useQuery({
    queryKey: ["studio-catalog-entry", ...scope, project, kind, selected],
    enabled: !!row,
    queryFn: ({ signal }) => {
      if (!row) throw new ApiError("not_found", 404);
      return studio.catalogEntry(project, kind, referenceOf(row), signal);
    },
  });
  const copy = useMutation({
    mutationFn: (reference: string) => navigator.clipboard.writeText(reference),
  });
  const contract = definition.data?.definition;
  const steps = workflowStepsSchema.safeParse(contract?.steps).data ?? [];
  const permission =
    typeof contract?.Permission === "string" ? contract.Permission : undefined;
  const heading = t(
    kind === "capability"
      ? "capabilities"
      : kind === "workflow"
        ? "workflows"
        : "plugins",
  );
  const taskAction = (reference: string, title: string) =>
    canWrite && (
      <Button
        type="button"
        size="sm"
        onClick={() =>
          onCreateTask({
            title,
            subjectItemId: plan?.id,
            checks: t("catalogTaskInstructions", {
              reference,
              kind,
              checks: plan?.checks?.slice(0, 11000) || t("none"),
            }),
          })
        }
      >
        <Plus />
        {t("createStudioTask")}
      </Button>
    );
  return (
    <Library
      entries={[
        ...planned.map((item) => ({
          id: item.catalogTarget!,
          title: item.title,
          subtitle: catalogStatus(item),
        })),
        ...(listing.data ?? [])
          .filter(
            (entry) =>
              !planned.some(
                (item) => item.catalogTarget === referenceOf(entry),
              ),
          )
          .map((item) => ({
            id: referenceOf(item),
            title: item.title || item.id,
            subtitle: `${t("catalogCompleted")} · ${item.version}`,
          })),
        ...(declarations.data ?? [])
          .filter(
            (entry) =>
              !planned.some(
                (item) => item.catalogTarget === referenceOf(entry),
              ) &&
              !listing.data?.some(
                (item) => referenceOf(item) === referenceOf(entry),
              ),
          )
          .map((entry) => ({
            id: referenceOf(entry),
            title: entry.id,
            subtitle: t("projectPluginManifest"),
          })),
      ]}
      action={
        canWrite && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="mt-1 w-full justify-start px-1.5"
            onClick={() => setPlanning(true)}
          >
            <Plus />
            {t(catalogAddLabel[kind])}
          </Button>
        )
      }
      selected={selected}
      onSelect={onSelect}
      heading={heading}
      loading={listing.isPending}
      fetching={listing.isFetching}
      error={listing.error}
      refresh={() => {
        void listing.refetch();
        if (kind === "plugin") void declarations.refetch();
        void refresh();
      }}
    >
      {row ? (
        <>
          <RecordHeader
            title={row.title || row.id}
            subtitle={row.id}
            badge={t("catalogCompleted")}
            action={
              <div className="flex flex-wrap gap-2">
                {taskAction(
                  referenceOf(row),
                  plan?.title ?? row.title ?? row.id,
                )}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => copy.mutate(referenceOf(row))}
                >
                  {copy.isSuccess && copy.variables === referenceOf(row) ? (
                    <Check />
                  ) : (
                    <Copy />
                  )}
                  {t(
                    copy.isSuccess && copy.variables === referenceOf(row)
                      ? "referenceCopied"
                      : "copyReference",
                  )}
                </Button>
              </div>
            }
          />
          <div className="grid max-w-5xl gap-5 p-5 sm:p-7">
            {manifest && <PluginManifestFields manifest={manifest} />}
            {row.description && (
              <DetailField label={t("definitionDescription")}>
                {row.description}
              </DetailField>
            )}
            {copy.isError && copy.variables === referenceOf(row) && (
              <p role="alert" className="text-sm text-destructive">
                {t("copyReferenceFailed")}
              </p>
            )}
            {permission && (
              <DetailField label={t("capabilityPermission")}>
                {permission}
              </DetailField>
            )}
            {steps.length > 0 && (
              <section className="min-w-0 space-y-2">
                <h3 className="text-sm font-medium">{t("workflowSteps")}</h3>
                <ol className="divide-y border-y">
                  {steps.map((step) => (
                    <li
                      key={step.id}
                      className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 py-2.5 text-sm [overflow-wrap:anywhere]"
                    >
                      <span>{step.id}</span>
                      {step.capability && (
                        <code className="text-xs text-muted-foreground">
                          {step.version
                            ? `${step.capability}@${step.version}`
                            : step.capability}
                        </code>
                      )}
                    </li>
                  ))}
                </ol>
              </section>
            )}
            <section className="min-w-0 space-y-3">
              {definition.isPending && <Loading />}
              {definition.isError && (
                <ErrorBanner
                  error={definition.error}
                  onRetry={() => void definition.refetch()}
                />
              )}
              {contract && (
                <details className="border-t pt-3">
                  <summary className="cursor-pointer text-sm font-semibold focus-visible:outline-ring">
                    {t("definition")}
                  </summary>
                  <pre className="mt-3 max-h-[36rem] overflow-auto rounded-lg bg-muted/60 p-4 text-xs leading-relaxed">
                    {JSON.stringify(contract, null, 2)}
                  </pre>
                </details>
              )}
            </section>
          </div>
        </>
      ) : plan ? (
        <>
          <RecordHeader
            title={plan.title}
            subtitle={plan.catalogTarget!}
            badge={catalogStatus(plan)}
            action={taskAction(plan.catalogTarget!, plan.title)}
          />
          {plan.checks && (
            <div className="p-5">
              <DetailField label={t("catalogDevelopmentRequirements")}>
                {plan.checks}
              </DetailField>
            </div>
          )}
        </>
      ) : manifest ? (
        <>
          <RecordHeader
            title={manifest.id}
            subtitle={t("projectPluginManifest")}
            badge={manifest.version}
            action={taskAction(referenceOf(manifest), manifest.id)}
          />
          <div className="grid max-w-3xl gap-5 p-5 sm:p-7">
            <PluginManifestFields manifest={manifest} />
          </div>
        </>
      ) : (
        <LibraryEmpty
          icon={
            kind === "capability" ? (
              <Braces />
            ) : kind === "workflow" ? (
              <GitBranch />
            ) : (
              <Puzzle />
            )
          }
        />
      )}
      {relatedTasks.length > 0 && (
        <div className="space-y-2 px-5 pb-5">
          <h3 className="text-sm font-medium">{t("linkedSessions")}</h3>
          {relatedTasks.map((task) => (
            <a
              key={task.id}
              className="block text-sm underline"
              href={studioWorkbenchPath(project, task.id)}
              target="_blank"
              rel="noopener noreferrer"
            >
              {task.title}
            </a>
          ))}
        </div>
      )}
      {planning && (
        <CatalogPlanDialog
          project={project}
          kind={kind}
          titleKey={catalogAddLabel[kind]}
          close={() => setPlanning(false)}
          saved={async (reference) => {
            onSelect(reference);
            await refresh();
          }}
        />
      )}
    </Library>
  );
}
