import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  ClipboardCheck,
  FolderGit2,
  FileText,
  GitBranch,
  MessageSquare,
  PanelRight,
  Puzzle,
  RefreshCw,
  Workflow,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Navigate, useSearchParams } from "react-router";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { useSession } from "@/features/administration/auth";
import { ErrorBanner, Loading } from "@/features/administration/shared";
import { studioProjectsOptions, studioSummaryOptions } from "./project-queries";
import { StudioApplicationAccess } from "./access";
import { studioProjectsPath } from "./routes";
import { initializationOptions } from "./initialization";
import { InitializationPanel } from "./project-details";
import NewTask, { type TaskDraft } from "./new-task";
import RunControls from "./run-controls";
import { RunOverview } from "./run-overview";
import { ProjectResources } from "./project-resources";
import { SessionWorkspace } from "./session-workspace";
import { AgentSettings } from "./agent-settings";
import { AgentPairing } from "./agent-pairing";
import type { AgentSessionStatus } from "./agent-connection";
import studioLogo from "./studio-logo.png";
import "./studio.css";

const views = [
  { id: "sessions", icon: MessageSquare },
  { id: "run", icon: Activity },
  { id: "initialization", icon: ClipboardCheck },
  { id: "requirements", icon: FileText },
  { id: "capabilities", icon: Workflow },
  { id: "workflows", icon: GitBranch },
  { id: "plugins", icon: Puzzle },
] as const;
type View = (typeof views)[number]["id"];

export function StudioPage() {
  const { t } = useTranslation("studio");
  useEffect(() => {
    const previousTitle = document.title;
    document.title = t("title");
    return () => {
      document.title = previousTitle;
    };
  }, [t]);
  const [params] = useSearchParams();
  if (!params.get("project"))
    return <Navigate to={studioProjectsPath} replace />;
  return (
    <StudioApplicationAccess>
      <StudioWorkbench />
    </StudioApplicationAccess>
  );
}

function StudioWorkbench() {
  const { t } = useTranslation("studio");
  const profile = useSession();
  const scope = [
    profile.tenantId,
    profile.account.id,
    profile.activeDepartmentId,
  ];
  const canWrite = profile.permissions.includes("studio:write");
  const canControl = profile.permissions.includes("studio:control");
  const client = useQueryClient();
  const [params, setParams] = useSearchParams();
  const projectParam = params.get("project");
  const view =
    views.find((item) => item.id === params.get("view"))?.id ?? "sessions";
  const entry = params.get("entry") ?? undefined;
  const [showInspector, setShowInspector] = useState(false);
  const openView = (nextView: View, item?: string) => {
    const next = new URLSearchParams(params);
    if (nextView !== view) next.delete("entry");
    next.set("view", nextView);
    if (item) next.set(nextView === "sessions" ? "task" : "entry", item);
    setParams(next);
  };
  const [creating, setCreating] = useState(false);
  const [taskDraft, setTaskDraft] = useState<TaskDraft>();
  const [agentStatuses, setAgentStatuses] = useState<
    Record<string, AgentSessionStatus>
  >({});
  const projects = useQuery(studioProjectsOptions(scope));
  const project = projects.data?.find((row) => String(row.id) === projectParam);
  const summary = useQuery(studioSummaryOptions(scope, project?.id));
  const initialization = useQuery(initializationOptions(scope, project?.id));
  const tasks =
    summary.data?.items.filter((row) => row.kind === "execution") ?? [];
  const sessionMode =
    params.get("sessionMode") === "all" ? "all" : "requirements";
  const showArchived = params.get("sessionArchived") === "1";
  const selectedTaskId = params.get("task");
  const visibleTasks = showArchived
    ? tasks
    : tasks.filter((row) => !row.archived);
  const task = selectedTaskId
    ? tasks.find((row) => String(row.id) === selectedTaskId)
    : sessionMode === "all"
      ? visibleTasks[0]
      : undefined;
  const requirement = summary.data?.requirements.find(
    (row) => row.id === task?.requirementId,
  );
  const refresh = () =>
    client.invalidateQueries({ queryKey: ["studio", ...scope] });

  const projectName =
    project?.name ||
    project?.root
      .split(/[\\/]+/)
      .filter(Boolean)
      .at(-1);
  const pending = projects.isPending || (!!project && summary.isPending);
  const error = projects.error ?? summary.error;
  const applyAgentStatuses = (statuses: AgentSessionStatus[]) => {
    setAgentStatuses(
      Object.fromEntries(statuses.map((status) => [status.sessionId, status])),
    );
  };
  const selectSession = (id: number) => {
    const sessionId = tasks.find((row) => row.id === id)?.dshSessionId;
    if (sessionId) {
      setAgentStatuses((current) => {
        const status = current[sessionId];
        if (!status || (!status.completed && !status.error)) return current;
        return {
          ...current,
          [sessionId]: { ...status, completed: false, error: false },
        };
      });
    }
    openView("sessions", String(id));
  };

  return (
    <main className="studio-workspace">
      <header className="studio-toolbar flex min-h-11 shrink-0 flex-wrap items-center justify-between gap-x-4 gap-y-1 px-3 py-1.5">
        <div className="flex min-w-0 flex-1 basis-48 items-center gap-5">
          <h1 className="studio-title studio-wordmark">
            <img src={studioLogo} alt={t("title")} />
            <img
              src={studioLogo}
              className="studio-wordmark-contrast"
              alt=""
              aria-hidden="true"
            />
          </h1>
          {project && (
            <div
              className="flex min-w-0 items-center gap-2 text-sm"
              title={project.root}
            >
              <FolderGit2
                className="size-4 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
              <span className="truncate font-medium">
                {projectName || project.root}
              </span>
            </div>
          )}
        </div>
        <div className="flex min-w-0 items-center gap-1.5">
          {project && (
            <AgentPairing
              key={scope.join(":")}
              sessionScope={scope.join(":")}
              accountId={profile.account.id}
              departmentId={profile.activeDepartmentId}
            />
          )}
          {project && canControl && (
            <RunControls
              key={project.id}
              project={project}
              initialized={
                initialization.data?.ready === true && !initialization.isError
              }
              onCreateRepairTask={
                canWrite
                  ? (draft) => {
                      setTaskDraft(draft);
                      setCreating(true);
                    }
                  : undefined
              }
            />
          )}
          {project && view === "sessions" && (
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={t("details")}
              title={t("details")}
              aria-pressed={showInspector}
              onClick={() => setShowInspector((open) => !open)}
            >
              <PanelRight />
            </Button>
          )}
        </div>
      </header>

      {project ? (
        <div
          className="studio-columns"
          data-inspector={
            showInspector && view === "sessions" ? "open" : "closed"
          }
        >
          <nav aria-label={t("sections")} className="studio-nav p-1.5">
            {views.map((item) => (
              <Button
                key={item.id}
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t(item.id)}
                title={t(item.id)}
                aria-current={item.id === view ? "page" : undefined}
                className="size-9 text-muted-foreground aria-[current=page]:bg-background aria-[current=page]:text-foreground"
                onClick={() => {
                  if (item.id !== view) openView(item.id);
                }}
              >
                <item.icon className="size-[18px]" />
              </Button>
            ))}
            <AgentSettings
              key={scope.join(":")}
              sessionScope={scope.join(":")}
            />
          </nav>
          <SessionWorkspace
            active={view === "sessions"}
            project={project}
            tasks={tasks}
            task={task}
            requirements={summary.data?.requirements ?? []}
            mode={sessionMode}
            onModeChange={(mode) => {
              const next = new URLSearchParams(params);
              if (mode === "all") next.set("sessionMode", mode);
              else {
                next.delete("sessionMode");
                if (!task?.requirementId) next.delete("task");
              }
              setParams(next);
            }}
            requirement={requirement}
            runs={summary.data?.runs ?? []}
            sessionScope={scope.join(":")}
            showInspector={showInspector}
            showArchived={showArchived}
            onShowArchived={(show) => {
              const next = new URLSearchParams(params);
              if (show) next.set("sessionArchived", "1");
              else next.delete("sessionArchived");
              setParams(next);
            }}
            canWrite={canWrite}
            agentStatuses={agentStatuses}
            onAgentStatus={applyAgentStatuses}
            pending={pending}
            loaded={summary.isSuccess}
            error={error}
            onCreate={(requirementId) => {
              setTaskDraft(
                requirementId === undefined
                  ? undefined
                  : { title: "", requirementId },
              );
              setCreating(true);
            }}
            onSelect={selectSession}
            refresh={() => void refresh()}
          />
          {view === "run" && (
            <RunOverview
              key={project.id}
              project={project.id}
              selected={entry}
              onSelect={(service) => openView("run", service)}
            />
          )}
          {view === "initialization" && (
            <InitializationPanel
              data={initialization.data}
              pending={initialization.isPending}
              fetching={initialization.isFetching}
              error={initialization.error}
              refresh={() => void initialization.refetch()}
            />
          )}
          <ProjectResources
            view={view}
            project={project.id}
            summary={summary}
            tasks={tasks}
            selected={entry}
            onSelect={(id) => openView(view, id)}
            onSession={(id) => openView("sessions", id)}
            refresh={refresh}
            canWrite={canWrite}
          />
        </div>
      ) : (
        <div className="grid flex-1 place-items-center px-4 py-12">
          <section className="w-full max-w-md">
            {pending ? (
              <Loading />
            ) : error ? (
              <div className="p-4">
                <ErrorBanner error={error} onRetry={() => void refresh()} />
              </div>
            ) : (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <FolderGit2 aria-hidden="true" />
                  </EmptyMedia>
                  <EmptyTitle>{t("projectUnavailable")}</EmptyTitle>
                  <EmptyDescription>
                    {t("projectUnavailableDetail")}
                  </EmptyDescription>
                </EmptyHeader>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => void refresh()}
                >
                  <RefreshCw />
                  {t("refresh")}
                </Button>
              </Empty>
            )}
          </section>
        </div>
      )}
      {creating && canWrite && project && (
        <NewTask
          open
          onClose={() => {
            setCreating(false);
            setTaskDraft(undefined);
          }}
          project={project}
          initial={taskDraft}
          fixedRequirement={summary.data?.requirements.find(
            (row) => row.id === taskDraft?.requirementId,
          )}
          requirements={summary.data?.requirements ?? []}
          onCreated={async (id) => {
            await refresh();
            openView("sessions", String(id));
          }}
        />
      )}
    </main>
  );
}
