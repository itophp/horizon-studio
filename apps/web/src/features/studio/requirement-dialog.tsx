import { zodResolver } from "@hookform/resolvers/zod";
import { LoaderCircle, Plus, Save } from "lucide-react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
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
import { NotificationEditor as MarkdownEditor } from "@/features/governance/notification-editor";
import { ErrorBanner } from "@/features/administration/shared";
import type { Requirement } from "./api";
import { RequirementParentPicker } from "./requirement-parent-picker";

const schema = z.object({
  title: z.string().trim().min(1).max(200),
  description: z.string().trim().max(12000),
  source: z.string().trim().max(200),
  parent: z.number().int().min(0),
});
type Fields = z.infer<typeof schema>;

export function RequirementDialog({
  project,
  requirements,
  requirement,
  parentRequirement,
  pending,
  error,
  close,
  submit,
}: {
  project: number;
  requirements: Requirement[];
  requirement?: Requirement;
  parentRequirement?: Requirement;
  pending: boolean;
  error: unknown;
  close: () => void;
  submit: (fields: Fields) => Promise<void>;
}) {
  const { t } = useTranslation("studio");
  const editing = requirement !== undefined;
  const creatingChild = !editing && parentRequirement !== undefined;
  const hasChildren =
    editing && requirements.some((row) => row.parentId === requirement.id);
  const form = useForm<Fields>({
    resolver: zodResolver(schema),
    defaultValues: {
      title: requirement?.title ?? "",
      description: requirement?.description ?? "",
      source: requirement?.source ?? "",
      parent: requirement?.parentId ?? 0,
    },
  });
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) close();
      }}
    >
      <DialogContent className="max-h-[88dvh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {t(
              editing
                ? "editRequirement"
                : creatingChild
                  ? "newSubRequirement"
                  : "newRequirement",
            )}
          </DialogTitle>
          <DialogDescription>
            {t(
              editing
                ? "requirementEditDialogDescription"
                : creatingChild
                  ? "requirementSubDialogDescription"
                  : "requirementDialogDescription",
            )}
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={form.handleSubmit(async (fields) => {
            await submit(fields).catch(() => {});
          })}
        >
          <div className="grid gap-2">
            <Label htmlFor="studio-requirement-title">
              {t("requirementTitle")}
            </Label>
            <Input
              id="studio-requirement-title"
              maxLength={200}
              required
              aria-invalid={!!form.formState.errors.title}
              aria-describedby={
                form.formState.errors.title
                  ? "studio-requirement-title-error"
                  : undefined
              }
              {...form.register("title")}
            />
            {form.formState.errors.title && (
              <p
                id="studio-requirement-title-error"
                className="text-sm text-destructive"
              >
                {t("requirementTitleRequired")}
              </p>
            )}
          </div>
          <div className="grid gap-2">
            <Label htmlFor="studio-requirement-description">
              {t("requirementDescription")}
            </Label>
            <Controller
              control={form.control}
              name="description"
              render={({ field }) => (
                <MarkdownEditor
                  inputId="studio-requirement-description"
                  ariaLabel={t("requirementDescription")}
                  value={field.value}
                  onChange={field.onChange}
                  onBlur={field.onBlur}
                  readOnly={pending}
                />
              )}
            />
            {form.formState.errors.description && (
              <p role="alert" className="text-sm text-destructive">
                {t("projectFieldInvalid")}
              </p>
            )}
          </div>
          <div className="grid gap-2">
            <Label htmlFor="studio-requirement-source">
              {t("requirementSource")}
            </Label>
            <Input
              id="studio-requirement-source"
              maxLength={200}
              {...form.register("source")}
            />
          </div>
          {creatingChild && (
            <div className="space-y-1">
              <p className="text-sm font-medium">{t("parentRequirement")}</p>
              <p className="text-sm break-words text-muted-foreground">
                {parentRequirement.title}
              </p>
            </div>
          )}
          {editing && (
            <div className="grid gap-2">
              <Label htmlFor="studio-requirement-parent">
                {t("parentRequirement")}
              </Label>
              {hasChildren ? (
                <p className="text-sm text-muted-foreground">
                  {t("requirementMoveBlocked")}
                </p>
              ) : (
                <Controller
                  control={form.control}
                  name="parent"
                  render={({ field }) => (
                    <RequirementParentPicker
                      id="studio-requirement-parent"
                      project={project}
                      value={field.value}
                      valueTitle={
                        requirements.find((row) => row.id === field.value)
                          ?.title
                      }
                      exclude={requirement.id}
                      disabled={pending}
                      onChange={field.onChange}
                    />
                  )}
                />
              )}
            </div>
          )}
          {error ? <ErrorBanner error={error} /> : null}
          <DialogFooter>
            <Button type="submit" disabled={pending}>
              {pending ? (
                <LoaderCircle className="motion-safe:animate-spin" />
              ) : editing ? (
                <Save />
              ) : (
                <Plus />
              )}
              {t(
                editing
                  ? "saveRequirement"
                  : creatingChild
                    ? "createSubRequirement"
                    : "createRequirement",
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
