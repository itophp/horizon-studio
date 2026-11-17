import { Archive, FileText, List, Plus } from "lucide-react";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ErrorBanner, Loading } from "@/features/administration/shared";
import type { AgentSessionStatus } from "./agent-connection";
import {
  stageMeta,
  studio,
  type Item,
  type Project,
  type Requirement,
  type Run,
} from "./api";
import Panes from "./panes";
import Session from "./session";
import { RequirementGroup, SessionRows } from "./session-rows";

type SessionMode = "requirements" | "all";

type Props = {
  active: boolean;
  project: Project;
  tasks: Item[];
  task?: Item;
  requirements: Requirement[];
  mode: SessionMode;
  onModeChange: (mode: SessionMode) => void;
  requirement?: Requirement;
  runs: Run[];
  sessionScope: string;
  showInspector: boolean;
  showArchived: boolean;
  onShowArchived: (show: boolean) => void;
  agentStatuses: Record<string, AgentSessionStatus>;
  onAgentStatus: (statuses: AgentSessionStatus[]) => void;
  canWrite: boolean;
  pending: boolean;
  loaded: boolean;
  error: unknown;
  onCreate: (requirementId?: number) => void;
  onSelect: (id: number) => void;
  refresh: () => void;
};

export function SessionWorkspace({
  active,
  project,
  tasks,
  task,
  requirements,
  mode,
  onModeChange,
  requirement,
  runs,
  sessionScope,
  showInspector,
  showArchived,
  onShowArchived,
  agentStatuses,
  onAgentStatus,
  canWrite,
  pending,
  loaded,
  error,
  onCreate,
  onSelect,
  refresh,
}: Props) {
  const { t } = useTranslation("studio");
  const pin = useMutation({
    mutationFn: ({ id, pinned }: { id: number; pinned: boolean }) =>
      studio.pinSession(project.id, id, pinned),
    onSuccess: () => refresh(),
  });
  const archive = useMutation({
    mutationFn: ({ id, archived }: { id: number; archived: boolean }) =>
      studio.archiveSession(project.id, id, archived),
    onSuccess: () => refresh(),
  });
  const busy = pin.isPending || archive.isPending;
  const actionError = pin.error ?? archive.error;
  const togglePin = (id: number, pinned: boolean) => {
    pin.reset();
    archive.reset();
    pin.mutate({ id, pinned });
  };
  const toggleArchive = (id: number, archived: boolean) => {
    pin.reset();
    archive.reset();
    archive.mutate({ id, archived });
  };
  // Pinned sessions come first from the server. Archived sessions stay out of
  // the default list and land after the active ones when the filter is on.
  const visible = showArchived
    ? [
        ...tasks.filter((item) => !item.archived),
        ...tasks.filter((item) => item.archived),
      ]
    : tasks.filter((item) => !item.archived);
  const archivedCount = tasks.filter((item) => item.archived).length;
  // Sessions are grouped only by top-level requirements: a session linked to a
  // sub-requirement belongs to the group of its root requirement.
  const rootRequirementID = (requirementID?: number) => {
    const row = requirements.find((item) => item.id === requirementID);
    return row?.parentId ? row.parentId : row?.id;
  };
  const rootRequirements = requirements.filter((row) => !row.parentId);
  return (
    <div className={active ? "contents" : "hidden"}>
      <aside
        aria-labelledby="studio-task-list"
        className="studio-tasks flex min-h-0 min-w-0 flex-col"
      >
        <header className="shrink-0 px-3 pt-3 pb-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h2
              id="studio-task-list"
              className="shrink-0 text-sm font-semibold"
            >
              {t("sessions")}
            </h2>
            <div className="ml-auto flex min-w-0 items-center gap-1">
              {(archivedCount > 0 || showArchived) && (
                <Button
                  type="button"
                  size="icon-xs"
                  variant={showArchived ? "secondary" : "ghost"}
                  aria-pressed={showArchived}
                  aria-label={t(showArchived ? "hideArchived" : "showArchived")}
                  title={t(showArchived ? "hideArchived" : "showArchived")}
                  onClick={() => onShowArchived(!showArchived)}
                >
                  <Archive />
                </Button>
              )}
              <Tabs
                value={mode}
                onValueChange={(value) => onModeChange(value as SessionMode)}
                className="gap-0"
              >
                <TabsList aria-label={t("sessionMode")}>
                  <TabsTrigger value="all" className="gap-1 px-1.5 text-xs">
                    <List className="size-3.5" aria-hidden />
                    {t("allSessions")}
                  </TabsTrigger>
                  <TabsTrigger
                    value="requirements"
                    className="gap-1 px-1.5 text-xs"
                  >
                    <FileText className="size-3.5" aria-hidden />
                    {t("requirementSessions")}
                  </TabsTrigger>
                </TabsList>
              </Tabs>
            </div>
          </div>
          {actionError ? (
            <p role="alert" className="mt-1 px-1 text-xs text-destructive">
              {t("sessionActionFailed")}
            </p>
          ) : null}
        </header>
        <div className="studio-task-scroll min-h-0 max-h-64 overflow-y-auto px-2 pb-2">
          {canWrite && mode === "all" && (
            <Button
              type="button"
              variant="ghost"
              className="mt-1 h-9 w-full justify-start px-1.5"
              onClick={() => onCreate()}
            >
              <Plus />
              {t("newTask")}
            </Button>
          )}
          {pending && <Loading />}
          {error ? <ErrorBanner error={error} onRetry={refresh} /> : null}
          {loaded && mode === "all" && visible.length === 0 && (
            <p className="px-3 py-6 text-center text-sm text-muted-foreground">
              {t("noTasks")}
            </p>
          )}
          {mode === "all" ? (
            <SessionRows
              tasks={visible}
              selectedId={task?.id}
              canWrite={canWrite}
              busy={busy}
              agentStatuses={agentStatuses}
              onSelect={onSelect}
              onPin={togglePin}
              onArchive={toggleArchive}
            />
          ) : (
            <>
              {loaded && rootRequirements.length === 0 && (
                <p className="px-3 py-6 text-center text-sm text-muted-foreground">
                  {t("noRequirements")}
                </p>
              )}
              {rootRequirements.map((row) => (
                <RequirementGroup
                  key={row.id}
                  requirement={row}
                  open={
                    task !== undefined &&
                    rootRequirementID(task.requirementId) === row.id
                  }
                  tasks={visible.filter(
                    (item) => rootRequirementID(item.requirementId) === row.id,
                  )}
                  selectedId={task?.id}
                  canWrite={canWrite}
                  busy={busy}
                  agentStatuses={agentStatuses}
                  onCreate={onCreate}
                  onSelect={onSelect}
                  onPin={togglePin}
                  onArchive={toggleArchive}
                />
              ))}
            </>
          )}
        </div>
      </aside>

      <section
        aria-labelledby="studio-conversation"
        className="studio-conversation flex min-h-0 min-w-0 flex-col overflow-hidden"
      >
        <header className="flex min-h-11 shrink-0 flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2">
          <h2
            id="studio-conversation"
            className="min-w-0 text-sm font-semibold [overflow-wrap:anywhere]"
          >
            {task?.title ?? t("agentSession")}
          </h2>
          {task && (
            <Badge variant="secondary">
              <span
                className={`size-1.5 shrink-0 rounded-full ${stageMeta[task.stage].dot}`}
                aria-hidden="true"
              />
              {t(stageMeta[task.stage].label)}
            </Badge>
          )}
        </header>
        <div className="studio-conversation-body flex min-h-96 min-w-0 flex-1 flex-col">
          <Session
            key={`${sessionScope}:${project.id}`}
            task={task}
            requirement={requirement}
            projectRoot={project.root}
            canWrite={canWrite}
            sessionScope={sessionScope}
            active={active}
            onAgentStatus={onAgentStatus}
          />
        </div>
      </section>

      {showInspector && (
        <aside
          aria-label={t("details")}
          className="studio-details flex min-h-0 min-w-0 flex-col"
        >
          <Panes
            key={project.id}
            project={project}
            task={task}
            requirement={requirement}
            runs={runs}
          />
        </aside>
      )}
    </div>
  );
}
