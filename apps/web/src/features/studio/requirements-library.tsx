import { useMutation } from "@tanstack/react-query";
import { ArrowUpRight, FileText, ListPlus, Pencil, Plus } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { studio, type Item, type Requirement } from "./api";
import {
  DetailField,
  Library,
  LibraryEmpty,
  RecordHeader,
  type Selection,
} from "./library";
import { MarkdownDisplay } from "@/features/application-runtime/markdown";
import { studioWorkbenchPath } from "./routes";
import type { TaskDraft } from "./new-task";
import { RequirementAttachments } from "./requirement-attachments";
import { RequirementDialog } from "./requirement-dialog";
import { RequirementTree } from "./requirement-tree";

type Editor =
  | { mode: "create"; parent?: Requirement }
  | { mode: "edit"; requirement: Requirement };

export function RequirementsLibrary({
  project,
  requirements,
  tasks,
  selected,
  onSelect,
  onSession,
  onCreateTask,
  refresh,
  loading,
  error,
  canWrite,
}: {
  project: number;
  requirements: Requirement[];
  tasks: Item[];
  selected?: string;
  onSelect: Selection;
  onSession?: Selection;
  onCreateTask: (draft: TaskDraft) => void;
  refresh: () => Promise<void>;
  loading: boolean;
  error: unknown;
  canWrite: boolean;
}) {
  const { t } = useTranslation("studio");
  const [editor, setEditor] = useState<Editor | null>(null);
  const requirement = requirements.find((row) => String(row.id) === selected);
  const parent =
    requirement?.parentId !== undefined
      ? requirements.find((row) => row.id === requirement?.parentId)
      : undefined;
  const children = requirement
    ? requirements.filter((row) => row.parentId === requirement.id)
    : [];
  const linked = requirement
    ? tasks.filter((row) => row.requirementId === requirement.id)
    : [];
  const badge = (row: Requirement) =>
    t(row.archived ? "requirementArchived" : "requirementActive");
  const create = useMutation({
    mutationFn: (input: {
      parent?: number;
      title: string;
      description?: string;
      source?: string;
    }) => studio.addRequirement({ project, ...input }),
  });
  const update = useMutation({
    mutationFn: (input: {
      id: number;
      parent?: number;
      title: string;
      description?: string;
      source?: string;
    }) => studio.editRequirement({ project, ...input }),
  });
  return (
    <Library
      entries={requirements.map((row) => ({
        id: String(row.id),
        title: row.title,
        subtitle: badge(row),
      }))}
      selected={selected}
      onSelect={onSelect}
      heading={t("requirements")}
      loading={loading}
      error={error}
      refresh={() => void refresh()}
      action={
        canWrite && (
          <Button
            type="button"
            variant="ghost"
            className="mt-1 h-9 w-full justify-start px-1.5"
            onClick={() => {
              create.reset();
              update.reset();
              setEditor({ mode: "create" });
            }}
          >
            <Plus />
            {t("newRequirement")}
          </Button>
        )
      }
      list={
        <RequirementTree
          requirements={requirements}
          selected={selected}
          onSelect={onSelect}
        />
      }
    >
      {requirement ? (
        <>
          <RecordHeader
            title={requirement.title}
            subtitle={t("requirements")}
            badge={badge(requirement)}
            action={
              canWrite &&
              !requirement.archived && (
                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      update.reset();
                      setEditor({ mode: "edit", requirement });
                    }}
                  >
                    <Pencil />
                    {t("editRequirement")}
                  </Button>
                  {!requirement.parentId && (
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        create.reset();
                        setEditor({ mode: "create", parent: requirement });
                      }}
                    >
                      <ListPlus />
                      {t("addSubRequirement")}
                    </Button>
                  )}
                  <Button
                    type="button"
                    size="sm"
                    onClick={() =>
                      onCreateTask({
                        title: requirement.title,
                        requirementId: requirement.id,
                      })
                    }
                  >
                    <Plus />
                    {t("createStudioTask")}
                  </Button>
                </div>
              )
            }
          />
          <div className="grid max-w-3xl gap-6 p-5 sm:p-7">
            {parent && (
              <DetailField label={t("parentRequirement")}>
                <Button
                  type="button"
                  variant="link"
                  className="h-auto p-0 text-sm"
                  onClick={() => onSelect(String(parent.id))}
                >
                  {parent.title}
                </Button>
              </DetailField>
            )}
            <DetailField label={t("requirementDescription")}>
              {requirement.description ? (
                <MarkdownDisplay value={requirement.description} />
              ) : (
                t("none")
              )}
            </DetailField>
            {requirement.source && (
              <DetailField label={t("requirementSource")}>
                {requirement.source}
              </DetailField>
            )}
            <RequirementAttachments
              project={project}
              requirement={requirement}
              canWrite={canWrite}
            />
            {children.length > 0 && (
              <section className="space-y-2 border-t pt-4">
                <h3 className="text-sm font-medium">{t("subRequirements")}</h3>
                <ul className="grid gap-2">
                  {children.map((child) => (
                    <li key={child.id}>
                      <Button
                        type="button"
                        variant="outline"
                        className="h-auto max-w-full justify-start gap-2 whitespace-normal text-left"
                        onClick={() => onSelect(String(child.id))}
                      >
                        <span className="min-w-0 truncate">{child.title}</span>
                        <Badge
                          variant={child.archived ? "outline" : "secondary"}
                          className="h-5 shrink-0 px-1.5 text-[0.65rem] font-normal"
                        >
                          {badge(child)}
                        </Badge>
                      </Button>
                    </li>
                  ))}
                </ul>
              </section>
            )}
            {linked.length > 0 && (
              <section className="space-y-2 border-t pt-4">
                <h3 className="text-sm font-medium">{t("linkedSessions")}</h3>
                <ul className="grid gap-2">
                  {linked.map((item) => (
                    <li key={item.id}>
                      {onSession ? (
                        <Button
                          type="button"
                          variant="outline"
                          className="h-auto max-w-full whitespace-normal text-left"
                          onClick={() => onSession(String(item.id))}
                        >
                          <ArrowUpRight />
                          {item.title}
                        </Button>
                      ) : (
                        <a
                          className="text-sm underline"
                          href={studioWorkbenchPath(project, item.id)}
                          target="_blank"
                          rel="noopener noreferrer"
                        >
                          {item.title}
                        </a>
                      )}
                    </li>
                  ))}
                </ul>
              </section>
            )}
          </div>
        </>
      ) : (
        <LibraryEmpty icon={<FileText />} />
      )}
      {editor && (
        <RequirementDialog
          project={project}
          requirements={requirements}
          requirement={editor.mode === "edit" ? editor.requirement : undefined}
          parentRequirement={
            editor.mode === "create" ? editor.parent : undefined
          }
          pending={editor.mode === "edit" ? update.isPending : create.isPending}
          error={editor.mode === "edit" ? update.error : create.error}
          close={() => setEditor(null)}
          submit={async (input) => {
            const { parent: parentID, ...fields } = input;
            if (editor.mode === "edit") {
              await update.mutateAsync({
                id: editor.requirement.id,
                parent: parentID,
                ...fields,
              });
            } else {
              const result = await create.mutateAsync({
                ...fields,
                parent: editor.parent?.id,
              });
              onSelect(String(result.id));
            }
            setEditor(null);
            await refresh();
          }}
        />
      )}
    </Library>
  );
}
