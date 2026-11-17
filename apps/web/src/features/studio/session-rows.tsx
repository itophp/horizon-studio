import {
  AlertCircle,
  Archive,
  ArchiveRestore,
  ChevronRight,
  LoaderCircle,
  Pin,
  Plus,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { AgentSessionStatus } from "./agent-connection";
import type { Item, Requirement } from "./api";

type SessionActions = {
  canWrite: boolean;
  busy: boolean;
  agentStatuses: Record<string, AgentSessionStatus>;
  onSelect: (id: number) => void;
  onPin: (id: number, pinned: boolean) => void;
  onArchive: (id: number, archived: boolean) => void;
};

// DSH owns the runtime facts; the row only renders the latest report. The
// green dot means an unseen finished turn and clears when the session opens.
function SessionIndicator({ status }: { status?: AgentSessionStatus }) {
  const { t } = useTranslation("studio");
  if (!status) return null;
  if (status.error) {
    return (
      <span
        role="img"
        aria-label={t("sessionFailed")}
        title={t("sessionFailed")}
        className="flex shrink-0 text-destructive"
      >
        <AlertCircle className="size-3.5" aria-hidden />
      </span>
    );
  }
  if (status.running) {
    return (
      <span
        role="status"
        aria-label={t("sessionRunning")}
        title={t("sessionRunning")}
        className="flex shrink-0 text-muted-foreground"
      >
        <LoaderCircle
          className="size-3.5 motion-safe:animate-spin"
          aria-hidden
        />
      </span>
    );
  }
  if (status.completed) {
    return (
      <span
        role="img"
        aria-label={t("sessionCompleted")}
        title={t("sessionCompleted")}
        className="flex shrink-0"
      >
        <span className="size-2 rounded-full bg-emerald-500" aria-hidden />
      </span>
    );
  }
  return null;
}

// The list carries no stage text: stage belongs to the conversation header and
// the task panes. The row keeps its title, the DSH runtime indicator, and the
// pin/archive actions that appear on hover only.
export function SessionRows({
  tasks,
  selectedId,
  canWrite,
  busy,
  agentStatuses,
  onSelect,
  onPin,
  onArchive,
}: {
  tasks: Item[];
  selectedId?: number;
} & SessionActions) {
  const { t } = useTranslation("studio");
  return (
    <ul className="grid content-start gap-0.5">
      {tasks.map((row) => {
        const status = row.dshSessionId
          ? agentStatuses[row.dshSessionId]
          : undefined;
        return (
          <li
            key={row.id}
            data-active={row.id === selectedId ? "true" : undefined}
            className="group/session relative flex min-w-0 items-center rounded-md bg-[var(--app-sidebar)] transition-colors hover:bg-sidebar-accent data-[active=true]:bg-sidebar-accent data-[active=true]:text-sidebar-accent-foreground"
          >
            <Button
              type="button"
              variant="ghost"
              aria-pressed={row.id === selectedId}
              className="h-auto min-h-10 min-w-0 flex-1 justify-start gap-2 rounded-md px-2 py-1.5 text-left whitespace-normal hover:bg-transparent aria-pressed:bg-transparent aria-pressed:text-inherit"
              onClick={() => onSelect(row.id)}
            >
              <span className="flex w-full min-w-0 items-center gap-2">
                <span
                  title={row.title}
                  className="block min-w-0 flex-1 truncate text-sm leading-5"
                >
                  {row.title}
                </span>
                <SessionIndicator status={status} />
              </span>
            </Button>
            {canWrite && (
              <span className="absolute inset-y-0 end-1 flex items-center gap-0.5 rounded-md bg-inherit px-0.5 opacity-0 transition-opacity group-hover/session:opacity-100 group-has-[:focus-visible]/session:opacity-100">
                <Button
                  type="button"
                  size="icon-xs"
                  variant="ghost"
                  disabled={busy}
                  aria-label={t(row.pinned ? "unpinSession" : "pinSession")}
                  title={t(row.pinned ? "unpinSession" : "pinSession")}
                  className={row.pinned ? "text-primary" : undefined}
                  onClick={() => onPin(row.id, !row.pinned)}
                >
                  <Pin className={row.pinned ? "fill-current" : undefined} />
                </Button>
                <Button
                  type="button"
                  size="icon-xs"
                  variant="ghost"
                  disabled={busy}
                  aria-label={t(
                    row.archived ? "unarchiveSession" : "archiveSession",
                  )}
                  title={t(
                    row.archived ? "unarchiveSession" : "archiveSession",
                  )}
                  onClick={() => onArchive(row.id, !row.archived)}
                >
                  {row.archived ? <ArchiveRestore /> : <Archive />}
                </Button>
              </span>
            )}
          </li>
        );
      })}
    </ul>
  );
}

// The requirement row owns the things a session row no longer shows: the
// requirement status directly after its name and the hover-only create entry.
export function RequirementGroup({
  requirement,
  open,
  tasks,
  selectedId,
  canWrite,
  busy,
  agentStatuses,
  onCreate,
  onSelect,
  onPin,
  onArchive,
}: {
  requirement: Requirement;
  open: boolean;
  tasks: Item[];
  selectedId?: number;
  onCreate: (requirementId: number) => void;
} & SessionActions) {
  const { t } = useTranslation("studio");
  return (
    <details
      open={open || undefined}
      className="group group/requirement rounded-md"
    >
      <summary className="flex cursor-pointer list-none items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-sidebar-accent focus-visible:outline-2 focus-visible:outline-ring [&::-webkit-details-marker]:hidden">
        <ChevronRight
          className="size-4 shrink-0 group-open:rotate-90"
          aria-hidden
        />
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="min-w-0 truncate" title={requirement.title}>
            {requirement.title}
          </span>
          <Badge
            variant={requirement.archived ? "outline" : "secondary"}
            className="h-5 shrink-0 px-1.5 text-[0.65rem] font-normal"
          >
            {t(
              requirement.archived
                ? "requirementArchived"
                : "requirementActive",
            )}
          </Badge>
        </span>
        <span className="min-w-0 flex-1" aria-hidden />
        {canWrite && !requirement.archived && (
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            aria-label={t("newTask")}
            title={t("newTask")}
            className="text-muted-foreground/70 opacity-0 transition-opacity group-hover/requirement:opacity-100 group-has-[:focus-visible]/requirement:opacity-100 hover:text-foreground focus-visible:opacity-100"
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              onCreate(requirement.id);
            }}
          >
            <Plus />
          </Button>
        )}
      </summary>
      <div className="pb-1 pl-4">
        <SessionRows
          tasks={tasks}
          selectedId={selectedId}
          canWrite={canWrite}
          busy={busy}
          agentStatuses={agentStatuses}
          onSelect={onSelect}
          onPin={onPin}
          onArchive={onArchive}
        />
        {tasks.length === 0 && (
          <p className="px-2 py-1 text-xs text-muted-foreground">
            {t("noRequirementSessions")}
          </p>
        )}
      </div>
    </details>
  );
}
