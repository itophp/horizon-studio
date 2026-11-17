import { useTranslation } from "react-i18next";
import { useState } from "react";
import { LoaderCircle, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  NativeSelect,
  NativeSelectOption,
} from "@/components/ui/native-select";
import { Textarea } from "@/components/ui/textarea";
import { errorText } from "@/features/administration/shared";
import { studio, type Project, type Requirement } from "./api";
import { studioWorkbenchPath } from "./routes";

export type TaskDraft = {
  title: string;
  checks?: string;
  requirementId?: number;
  subjectItemId?: number;
};

export default function NewTask({
  open,
  onClose,
  project,
  requirements,
  initial,
  fixedRequirement,
  onCreated,
  openInStudio = false,
}: {
  open: boolean;
  onClose: () => void;
  project?: Pick<Project, "id">;
  requirements: Requirement[];
  initial?: TaskDraft;
  fixedRequirement?: Requirement;
  openInStudio?: boolean;
  onCreated: (taskId: number) => Promise<void> | void;
}) {
  const { t } = useTranslation("studio");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // Top-level requirements first, then their sub-requirements in tree order.
  const requirementOptions = [
    ...requirements
      .filter((row) => !row.parentId)
      .sort((left, right) => right.id - left.id),
    ...requirements
      .filter((row) => row.parentId)
      .sort(
        (left, right) =>
          (left.parentId ?? 0) - (right.parentId ?? 0) || right.id - left.id,
      ),
  ];
  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!project || busy) return;
    const data = Object.fromEntries(
      new FormData(event.currentTarget),
    ) as Record<string, string>;
    const tab = openInStudio ? window.open("about:blank", "_blank") : null;
    if (tab) tab.opener = null;
    let openedTask = false;
    setBusy(true);
    setError("");
    studio
      .addItem({
        project: project.id,
        kind: "execution",
        title: data.title,
        requirementId: data.requirementId
          ? Number(data.requirementId)
          : undefined,
        checks: data.checks || undefined,
        subjectItemId: initial?.subjectItemId,
      })
      .then(async ({ id }) => {
        if (openInStudio) {
          const href = studioWorkbenchPath(project.id, id);
          openedTask = true;
          if (tab && !tab.closed) tab.location.replace(href);
          else window.location.assign(href);
        }
        await onCreated(id);
        onClose();
      })
      .catch((e) => {
        if (tab && !tab.closed && !openedTask) tab.close();
        setError(errorText(e, t));
      })
      .finally(() => setBusy(false));
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!value) onClose();
      }}
    >
      <DialogContent className="sm:max-w-[560px] max-h-[82vh] overflow-auto">
        <DialogHeader>
          <DialogTitle>{t("newTask")}</DialogTitle>
          <DialogDescription>{t("taskDescription")}</DialogDescription>
        </DialogHeader>
        <form className="space-y-4" onSubmit={submit}>
          <div className="space-y-2">
            <Label htmlFor="title">{t("taskTitle")}</Label>
            <Input
              id="title"
              name="title"
              required
              maxLength={200}
              defaultValue={initial?.title}
              key={initial?.title}
            />
          </div>
          {fixedRequirement ? (
            <div className="space-y-1">
              <p className="text-sm font-medium">{t("linkedRequirement")}</p>
              <p className="text-sm break-words text-muted-foreground">
                {fixedRequirement.title}
              </p>
              <input
                type="hidden"
                name="requirementId"
                value={fixedRequirement.id}
              />
            </div>
          ) : (
            <div className="space-y-2">
              <Label htmlFor="studio-requirement">
                {t("linkedRequirement")}
              </Label>
              <NativeSelect
                id="studio-requirement"
                name="requirementId"
                defaultValue={initial?.requirementId ?? ""}
                className="w-full"
              >
                <NativeSelectOption value="">{t("none")}</NativeSelectOption>
                {requirementOptions
                  .filter((row) => !row.archived)
                  .map((row) => (
                    <NativeSelectOption key={row.id} value={row.id}>
                      {row.parentId ? `— ${row.title}` : row.title}
                    </NativeSelectOption>
                  ))}
              </NativeSelect>
            </div>
          )}
          <div className="space-y-2">
            <Label htmlFor="checks">{t("criteria")}</Label>
            <Textarea
              id="checks"
              name="checks"
              rows={5}
              maxLength={12000}
              placeholder={t("criteriaPlaceholder")}
              defaultValue={initial?.checks}
              key={initial?.checks}
            />
          </div>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button type="submit" disabled={busy}>
              {busy ? (
                <LoaderCircle size={15} className="animate-spin" />
              ) : (
                <Plus size={15} />
              )}{" "}
              {t("createTask")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
