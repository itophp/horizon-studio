import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  ArrowUpRight,
  CheckCircle2,
  CircleAlert,
  Pencil,
  RefreshCw,
} from "lucide-react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  ErrorBanner,
  Loading,
  PageHeading,
} from "@/features/administration/shared";
import type { Initialization, Project } from "./api";
import {
  isRunning,
  isStopped,
  runningServiceURL,
  serviceStatusOptions,
} from "./service-status";
import { ProjectEditor, studioProjectName } from "./project-list";
import { studioProjectsPath, studioWorkbenchPath } from "./routes";

export function ProjectDetails({
  project,
  initialization,
  canWrite,
  refresh,
}: {
  project: Project;
  initialization?: Initialization;
  canWrite: boolean;
  refresh: () => Promise<void>;
}) {
  const { t } = useTranslation("studio");
  const [editing, setEditing] = useState(false);
  const status = useQuery(serviceStatusOptions(project.id));
  const rows = status.isError ? [] : (status.data ?? []);
  const origins = [
    {
      label: t("projectAPI"),
      href: runningServiceURL(rows, "backend", "/api/health"),
    },
    { label: t("projectWeb"), href: runningServiceURL(rows, "frontend") },
  ];
  const running = rows.filter(isRunning).length;
  const label =
    running > 0
      ? t("servicesRunning", { running, total: rows.length })
      : rows.length > 0 && rows.every(isStopped)
        ? t("runStopped")
        : t("unknownStatus");
  return (
    <>
      <PageHeading title={studioProjectName(project)} subtitle={project.root}>
        <Link
          to={studioProjectsPath}
          className={buttonVariants({ variant: "outline" })}
        >
          <ArrowLeft />
          {t("projectManagement")}
        </Link>
        <a
          href={studioWorkbenchPath(project.id)}
          target="_blank"
          rel="noopener noreferrer"
          className={buttonVariants({ variant: "outline" })}
        >
          <ArrowUpRight />
          {t("openStudio")}
        </a>
        {canWrite && (
          <Button onClick={() => setEditing(true)}>
            <Pencil />
            {t("projectEdit")}
          </Button>
        )}
      </PageHeading>
      <section className="admin-panel mb-5 flex flex-wrap items-center justify-between gap-x-6 gap-y-3 p-4">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="outline">
            {t("projectRunStatus", { state: label })}
          </Badge>
          <Badge variant="outline">{t("projectDeploymentStatus")}</Badge>
          <Badge variant={initialization?.ready ? "secondary" : "outline"}>
            {t(
              initialization?.ready
                ? "projectInitialized"
                : initialization
                  ? "projectNotInitialized"
                  : "projectNotChecked",
            )}
          </Badge>
        </div>
        <dl className="flex min-w-0 flex-wrap gap-x-6 gap-y-1 text-xs text-muted-foreground">
          {origins.map(
            ({ label, href }) =>
              href && (
                <div
                  key={label}
                  className="flex flex-wrap items-baseline gap-x-2"
                >
                  <dt>{label}</dt>
                  <dd>
                    <a
                      href={href}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="font-mono underline underline-offset-2"
                    >
                      {new URL(href).origin}
                    </a>
                  </dd>
                </div>
              ),
          )}
        </dl>
        {project.developmentBrief && (
          <div className="w-full border-t pt-3">
            <h2 className="text-sm font-medium">
              {t("projectDevelopmentBrief")}
            </h2>
            <p className="mt-1 whitespace-pre-wrap text-sm text-muted-foreground">
              {project.developmentBrief}
            </p>
          </div>
        )}
      </section>
      {editing && (
        <ProjectEditor
          mode="edit"
          project={project}
          close={() => setEditing(false)}
          saved={async () => refresh()}
        />
      )}
    </>
  );
}

const labels = {
  go: "initGo",
  api_address: "initAPI",
  postgresql: "initDatabase",
  redis: "initRedis",
  node: "initNode",
  web_dependencies: "initWebDependencies",
  web_port: "initWebPort",
  environment: "initEnvironment",
  horizon_cli: "initCLI",
} as const;
type CheckName = keyof typeof labels;
const initCommands: Partial<Record<CheckName, string>> = {
  web_dependencies: "pnpm install --frozen-lockfile",
  horizon_cli: "go build -o horizon-cli ./foundation/cmd/horizon-cli",
  postgresql: "horizon-cli env configure",
};
export function InitializationPanel({
  data,
  pending,
  fetching,
  error,
  refresh,
}: {
  data?: Initialization;
  pending: boolean;
  fetching: boolean;
  error: unknown;
  refresh: () => void;
}) {
  const { t } = useTranslation("studio");
  return (
    <section className="studio-complete overflow-auto px-5 py-5 sm:px-7">
      <div className="max-w-4xl space-y-4">
        <header className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h2 className="text-lg font-semibold">{t("initialization")}</h2>
            <p className="text-sm text-muted-foreground">
              {t("projectInitHint")}
            </p>
          </div>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={fetching}
            onClick={refresh}
          >
            <RefreshCw
              className={fetching ? "motion-safe:animate-spin" : undefined}
            />
            {t("refresh")}
          </Button>
        </header>
        {pending && <Loading />}
        {error ? <ErrorBanner error={error} onRetry={refresh} /> : null}
        {data && (
          <ul className="divide-y rounded-lg bg-muted/40 px-4">
            {data.result.map((check, index) => {
              const key = labels[check.name as CheckName];
              const command = initCommands[check.name as CheckName];
              const passed = check.status === "ok";
              return (
                <li
                  key={`${check.name}:${index}`}
                  className="flex items-start justify-between gap-3 py-3"
                >
                  <div className="space-y-1">
                    <span className="flex items-center gap-2 text-sm font-medium">
                      {passed ? (
                        <CheckCircle2 className="size-4 text-emerald-600" />
                      ) : (
                        <CircleAlert className="size-4 text-muted-foreground" />
                      )}
                      {key ? t(key) : check.name}
                    </span>
                    {!passed && command && (
                      <code className="text-xs text-muted-foreground">
                        {command}
                      </code>
                    )}
                  </div>
                  <Badge
                    variant={
                      !passed && check.required ? "destructive" : "secondary"
                    }
                  >
                    {t(
                      passed
                        ? "initPassed"
                        : check.required
                          ? "initBlocked"
                          : "initOptional",
                    )}
                  </Badge>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </section>
  );
}
