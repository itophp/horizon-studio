import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import {
  ArrowUpRight,
  FolderOpen,
  FolderPlus,
  Link2,
  LoaderCircle,
  Pencil,
  Sparkles,
} from "lucide-react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router";
import { z } from "zod";
import { Button, buttonVariants } from "@/components/ui/button";
import { studioProjectPath, studioWorkbenchPath } from "./routes";
import { Badge } from "@/components/ui/badge";
import { initializationOptions } from "./initialization";
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
import { Textarea } from "@/components/ui/textarea";
import {
  DataTable,
  ErrorBanner,
  Loading,
  PageHeading,
  type AdminColumn,
} from "@/features/identity/shared";
import { studio, type Project, type ProjectInput } from "./api";

export function studioProjectName(project: Project) {
  return (
    project.name ||
    project.root
      .split(/[\\/]+/)
      .filter(Boolean)
      .at(-1) ||
    project.root
  );
}

const projectForm = z.object({
  name: z.string().trim().max(200),
  path: z.string().trim().min(1).max(1024),
  developmentBrief: z.string().trim().max(4000),
  repository: z.string().trim().max(500),
});
type Fields = z.infer<typeof projectForm>;
type Mode = "link" | "create" | "edit";

export function ProjectEditor({
  mode,
  project,
  close,
  saved,
}: {
  mode: Mode;
  project?: Project;
  close: () => void;
  saved: (id: number) => Promise<void>;
}) {
  const { t } = useTranslation("studio");
  const form = useForm<Fields>({
    resolver: zodResolver(projectForm),
    defaultValues: {
      name: project?.name ?? "",
      path: project?.root ?? "",
      developmentBrief: project?.developmentBrief ?? "",
      repository: "",
    },
  });
  const operation = useMutation({
    mutationFn: (input: ProjectInput) =>
      mode === "create"
        ? studio.createProject(input)
        : studio.linkProject(input),
  });
  const heading = t(
    mode === "create"
      ? "projectCreate"
      : mode === "edit"
        ? "projectEdit"
        : "projectLink",
  );
  const fields = [
    { name: "name", label: "projectName", type: "text", length: 200 },
    { name: "path", label: "projectRoot", type: "text", length: 1024 },
    ...(mode === "create"
      ? ([
          {
            name: "repository",
            label: "projectRepository",
            type: "text",
            length: 500,
          },
        ] as const)
      : []),
  ] as const;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !operation.isPending) close();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{heading}</DialogTitle>
          <DialogDescription>{t("projectFormHint")}</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={form.handleSubmit(async (value) => {
            try {
              const result = await operation.mutateAsync({
                ...value,
                repository: value.repository || undefined,
              });
              await saved(result.id);
              close();
            } catch {
              /* The mutation error is displayed below. */
            }
          })}
        >
          {fields.map((field) => {
            const id = `studio-project-${field.name}`;
            const error = form.formState.errors[field.name];
            return (
              <div key={field.name} className="grid gap-1.5">
                <Label htmlFor={id}>{t(field.label)}</Label>
                <Input
                  id={id}
                  type={field.type}
                  maxLength={field.length}
                  readOnly={mode === "edit" && field.name === "path"}
                  aria-invalid={!!error}
                  aria-describedby={error ? `${id}-error` : undefined}
                  {...form.register(field.name)}
                />
                {error && (
                  <p id={`${id}-error`} className="text-xs text-destructive">
                    {t("projectFieldInvalid")}
                  </p>
                )}
              </div>
            );
          })}
          <div className="grid gap-1.5">
            <Label htmlFor="studio-project-development-brief">
              {t("projectDevelopmentBrief")}
            </Label>
            <Textarea
              id="studio-project-development-brief"
              rows={5}
              maxLength={4000}
              aria-describedby="studio-project-development-brief-hint"
              {...form.register("developmentBrief")}
            />
            <p
              id="studio-project-development-brief-hint"
              className="text-xs text-muted-foreground"
            >
              {t("projectDevelopmentBriefHint")}
            </p>
            {form.formState.errors.developmentBrief && (
              <p role="alert" className="text-xs text-destructive">
                {t("projectFieldInvalid")}
              </p>
            )}
          </div>
          {operation.error && <ErrorBanner error={operation.error} />}
          <DialogFooter>
            <Button type="submit" disabled={operation.isPending}>
              {operation.isPending ? (
                <LoaderCircle className="motion-safe:animate-spin" />
              ) : mode === "edit" ? (
                <Pencil />
              ) : mode === "create" ? (
                <FolderPlus />
              ) : (
                <Link2 />
              )}
              {heading}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ProjectInitialization({
  project,
  scope,
}: {
  project: number;
  scope: readonly (string | number | undefined)[];
}) {
  const { t } = useTranslation("studio");
  const state = useQuery(initializationOptions(scope, project));
  const ready = state.data?.ready && !state.isError;
  return (
    <Link to={studioProjectPath(project, "initialization")}>
      <Badge variant={ready ? "secondary" : "outline"}>
        {t(
          ready
            ? "projectInitialized"
            : state.isSuccess
              ? "projectNotInitialized"
              : "projectNotChecked",
        )}
      </Badge>
    </Link>
  );
}

export function StudioProjectList({
  projects,
  pending,
  error,
  canWrite,
  canAnalyze,
  scope,
  refresh,
}: {
  projects: Project[];
  pending: boolean;
  error: unknown;
  canWrite: boolean;
  canAnalyze: boolean;
  scope: readonly (string | number | undefined)[];
  refresh: () => Promise<void>;
}) {
  const { t, i18n } = useTranslation("studio");
  const navigate = useNavigate();
  const [editor, setEditor] = useState<"link" | "create">();
  const analysis = useMutation({
    mutationFn: (project: number) =>
      studio.analyzeProject(
        project,
        i18n.resolvedLanguage === "zh-CN" ? "zh-CN" : "en",
      ),
    onSuccess: refresh,
  });
  const columns: AdminColumn<Project>[] = [
    {
      id: "name",
      header: t("projectName"),
      cell: (row) => (
        <div className="min-w-44">
          <span className="font-medium">{studioProjectName(row)}</span>
          <p
            className="max-w-80 truncate font-mono text-xs text-muted-foreground"
            title={row.root}
          >
            {row.root}
          </p>
        </div>
      ),
    },
    {
      id: "requirements",
      header: t("projectRequirementCounts"),
      cell: (row) => (
        <span className="tabular-nums">
          {t("projectRequirementRatio", {
            pending: row.requirementsPending ?? 0,
            total: row.requirementsTotal ?? 0,
          })}
        </span>
      ),
    },
    {
      id: "initialization",
      header: t("initialization"),
      cell: (row) => <ProjectInitialization project={row.id} scope={scope} />,
    },
    {
      id: "analysis",
      header: t("projectAnalysis"),
      cell: (row) => (
        <div className="flex min-w-48 max-w-xl flex-wrap items-center justify-between gap-2">
          <p
            className="line-clamp-2 min-w-32 flex-1 whitespace-normal text-muted-foreground"
            title={row.aiAnalysis}
          >
            {row.aiAnalysis || t("projectAnalysisEmpty")}
          </p>
          {canAnalyze && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={analysis.isPending}
              onClick={() => analysis.mutate(row.id)}
            >
              {analysis.isPending && analysis.variables === row.id ? (
                <LoaderCircle className="motion-safe:animate-spin" />
              ) : (
                <Sparkles />
              )}
              {t("projectAnalyze")}
            </Button>
          )}
        </div>
      ),
    },
    {
      id: "actions",
      header: t("projectActions"),
      cell: (row) => (
        <div className="flex items-center gap-1">
          <Link
            to={studioWorkbenchPath(row.id)}
            target="_blank"
            rel="noopener noreferrer"
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            <ArrowUpRight />
            {t("openStudio")}
          </Link>
          <Link
            to={studioProjectPath(row.id)}
            className={buttonVariants({ variant: "ghost", size: "sm" })}
          >
            <FolderOpen />
            {t("projectTabs")}
          </Link>
        </div>
      ),
    },
  ];
  return (
    <section>
      <PageHeading
        title={t("projectManagement")}
        subtitle={t("projectManagementScope")}
      >
        {canWrite && (
          <>
            <Button
              type="button"
              variant="outline"
              onClick={() => setEditor("link")}
            >
              <Link2 />
              {t("projectLink")}
            </Button>
            <Button type="button" onClick={() => setEditor("create")}>
              <FolderPlus />
              {t("projectCreate")}
            </Button>
          </>
        )}
      </PageHeading>
      <div className="space-y-5">
        {error ? (
          <ErrorBanner error={error} onRetry={() => void refresh()} />
        ) : null}
        {analysis.error && <ErrorBanner error={analysis.error} />}
        {pending ? (
          <Loading />
        ) : projects.length ? (
          <div className="admin-panel overflow-hidden">
            <DataTable rows={projects} columns={columns} />
          </div>
        ) : (
          <p className="py-8 text-sm text-muted-foreground">
            {t("noProjectDetail")}
          </p>
        )}
      </div>
      {editor && (
        <ProjectEditor
          mode={editor}
          close={() => setEditor(undefined)}
          saved={async (id) => {
            await refresh();
            void navigate(studioProjectPath(id, "initialization"));
          }}
        />
      )}
    </section>
  );
}
