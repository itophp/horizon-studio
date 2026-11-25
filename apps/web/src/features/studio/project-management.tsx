import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ClipboardCheck,
  FileText,
  GitBranch,
  Puzzle,
  Workflow,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useParams, useSearchParams } from "react-router";
import { AdminPageTabs } from "@/features/identity/page-tabs";
import { useSession } from "@/features/identity/auth";
import { ErrorBanner, Loading } from "@/features/identity/shared";
import { StudioApplicationAccess } from "./access";
import type { Project } from "./api";
import {
  studioProjectsOptions,
  studioSummaryOptions,
  type StudioScope,
} from "./project-queries";
import { initializationOptions } from "./initialization";
import { InitializationPanel, ProjectDetails } from "./project-details";
import { StudioProjectList } from "./project-list";
import { ProjectResources } from "./project-resources";
import "./studio.css";

const detailTabs = [
  { id: "requirements", icon: FileText },
  { id: "capabilities", icon: Workflow },
  { id: "workflows", icon: GitBranch },
  { id: "plugins", icon: Puzzle },
  { id: "initialization", icon: ClipboardCheck },
] as const;

export function StudioProjectManagementPage() {
  return (
    <StudioApplicationAccess>
      <ProjectManagement />
    </StudioApplicationAccess>
  );
}

function ProjectManagement() {
  const profile = useSession();
  const { t } = useTranslation("studio");
  const { projectId } = useParams();
  const client = useQueryClient();
  const scope = [
    profile.tenantId,
    profile.account.id,
    profile.identityBindingId,
  ];
  const projects = useQuery(studioProjectsOptions(scope));
  const canWrite = profile.permissions.includes("studio:write");
  const refresh = () =>
    client.invalidateQueries({ queryKey: ["studio", ...scope] });

  if (projectId === undefined)
    return (
      <StudioProjectList
        projects={projects.data ?? []}
        pending={projects.isPending}
        error={projects.error}
        canWrite={canWrite}
        canAnalyze={canWrite && profile.permissions.includes("assistant:use")}
        scope={scope}
        refresh={refresh}
      />
    );

  if (projects.isPending) return <Loading />;
  if (projects.isError)
    return (
      <ErrorBanner error={projects.error} onRetry={() => void refresh()} />
    );
  const project = projects.data.find((row) => String(row.id) === projectId);
  if (!project) return <h1>{t("projectUnavailable")}</h1>;
  return (
    <ManagedProject
      project={project}
      scope={scope}
      canWrite={canWrite}
      refresh={refresh}
    />
  );
}

function ManagedProject({
  project,
  scope,
  canWrite,
  refresh,
}: {
  project: Project;
  scope: StudioScope;
  canWrite: boolean;
  refresh: () => Promise<void>;
}) {
  const { t } = useTranslation("studio");
  const [params, setParams] = useSearchParams();
  const view =
    detailTabs.find((tab) => tab.id === params.get("view"))?.id ??
    "requirements";
  const selected = params.get("entry") ?? undefined;
  const summary = useQuery(studioSummaryOptions(scope, project.id));
  const initialization = useQuery(initializationOptions(scope, project.id));
  return (
    <section className="[overflow-wrap:anywhere]">
      <ProjectDetails
        project={project}
        initialization={
          initialization.isError ? undefined : initialization.data
        }
        canWrite={canWrite}
        refresh={refresh}
      />
      <AdminPageTabs
        tabs={detailTabs.map((tab) => ({
          id: tab.id,
          label: t(tab.id),
          icon: <tab.icon size={15} aria-hidden="true" />,
        }))}
        value={view}
        onValueChange={(next) =>
          setParams((previous) => {
            const nextParams = new URLSearchParams(previous);
            nextParams.set("view", next);
            nextParams.delete("entry");
            return nextParams;
          })
        }
        label={t("projectTabs")}
      >
        <div className="admin-panel overflow-hidden">
          {view === "initialization" ? (
            <InitializationPanel
              data={initialization.data}
              pending={initialization.isPending}
              fetching={initialization.isFetching}
              error={initialization.error}
              refresh={() => void initialization.refetch()}
            />
          ) : (
            <div className="studio-project-resources grid min-h-80 md:grid-cols-[minmax(12rem,16rem)_minmax(0,1fr)]">
              <ProjectResources
                view={view}
                project={project.id}
                summary={summary}
                tasks={
                  summary.data?.items.filter(
                    (row) => row.kind === "execution",
                  ) ?? []
                }
                selected={selected}
                onSelect={(entry) => setParams({ view, entry })}
                refresh={refresh}
                canWrite={canWrite}
              />
            </div>
          )}
        </div>
      </AdminPageTabs>
    </section>
  );
}
