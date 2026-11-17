import { useState, type ReactNode } from "react";
import NewTask, { type TaskDraft } from "./new-task";
import type { UseQueryResult } from "@tanstack/react-query";
import type { Item, Summary } from "./api";
import { CatalogLibrary } from "./catalog-library";
import { RequirementsLibrary } from "./requirements-library";

export function ProjectResources({
  view,
  project,
  summary,
  tasks,
  selected,
  onSelect,
  onSession,
  refresh,
  canWrite,
}: {
  view: string;
  project: number;
  summary: UseQueryResult<Summary>;
  tasks: Item[];
  selected?: string;
  onSelect: (entry: string) => void;
  onSession?: (id: string) => void;
  refresh: () => Promise<void>;
  canWrite: boolean;
}) {
  const [draft, setDraft] = useState<TaskDraft>();
  let panel: ReactNode;
  switch (view) {
    case "requirements":
      panel = (
        <RequirementsLibrary
          project={project}
          requirements={summary.data?.requirements ?? []}
          tasks={tasks}
          selected={selected}
          onSelect={onSelect}
          onSession={onSession}
          onCreateTask={setDraft}
          refresh={refresh}
          loading={summary.isPending}
          error={summary.error}
          canWrite={canWrite}
        />
      );
      break;
    case "capabilities":
    case "workflows":
    case "plugins": {
      const kind =
        view === "capabilities"
          ? "capability"
          : view === "workflows"
            ? "workflow"
            : "plugin";
      panel = (
        <CatalogLibrary
          key={kind}
          project={project}
          kind={kind}
          selected={selected}
          onSelect={onSelect}
          items={summary.data?.items ?? []}
          canWrite={canWrite}
          refresh={refresh}
          onCreateTask={setDraft}
        />
      );
      break;
    }
    default:
      return null;
  }
  return (
    <>
      {panel}
      {draft && (
        <NewTask
          open
          project={{ id: project }}
          requirements={summary.data?.requirements ?? []}
          initial={draft}
          onClose={() => setDraft(undefined)}
          openInStudio={!onSession}
          onCreated={async (id) => {
            await refresh();
            onSession?.(String(id));
          }}
        />
      )}
    </>
  );
}
