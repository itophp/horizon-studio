import { useTranslation } from "react-i18next";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { runningServiceURL, serviceStatusOptions } from "./service-status";
import {
  FolderTree,
  GitCompare,
  ListChecks,
  LoaderCircle,
  Monitor,
  Terminal,
  Server,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { errorText } from "@/features/identity/shared";
import {
  studio,
  type Item,
  type Project,
  type Requirement,
  type Run,
} from "./api";

type Pane = "plan" | "diff" | "files" | "terminal" | "preview";

const tabs: { id: Pane; label: string; icon: React.ReactNode }[] = [
  { id: "plan", label: "plan", icon: <ListChecks size={14} /> },
  { id: "diff", label: "changes", icon: <GitCompare size={14} /> },
  { id: "files", label: "files", icon: <FolderTree size={14} /> },
  { id: "terminal", label: "logs", icon: <Terminal size={14} /> },
  { id: "preview", label: "preview", icon: <Monitor size={14} /> },
];

const pending = (label: string, detail: string) => (
  <div className="space-y-2 rounded-lg border border-dashed p-4 text-sm">
    <p className="font-medium">{label}</p>
    <p className="text-muted-foreground">{detail}</p>
  </div>
);

export default function Panes({
  project,
  task,
  requirement,
  runs,
}: {
  project?: Project;
  task?: Item;
  requirement?: Requirement;
  runs: Run[];
}) {
  const { t } = useTranslation("studio");
  const [pane, setPane] = useState<Pane>("plan");
  const [log, setLog] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  const services = useQuery({
    ...serviceStatusOptions(project?.id),
    enabled: project !== undefined && pane === "preview",
  });
  const preview = runningServiceURL(
    services.isError ? [] : (services.data ?? []),
    "frontend",
  );

  const loadLog = (service: string) => {
    if (!project) return;
    setBusy(service);
    setError("");
    studio
      .log(project.id, service)
      .then(setLog)
      .catch((e) => setError(errorText(e, t)))
      .finally(() => setBusy(""));
  };

  return (
    <Tabs
      value={pane}
      onValueChange={(value: Pane) => setPane(value)}
      className="min-h-0 flex-1 gap-0 overflow-hidden"
    >
      <TabsList
        aria-label={t("details")}
        className="mx-2 mt-2 w-auto justify-between"
      >
        {tabs.map((row) => (
          <TabsTrigger
            key={row.id}
            value={row.id}
            className="gap-1 px-1 text-xs"
          >
            {row.icon}
            {t(row.label)}
          </TabsTrigger>
        ))}
      </TabsList>
      <TabsContent value={pane} className="min-h-0 overflow-y-auto px-4 py-5">
        {error && (
          <p
            role="alert"
            className="mb-3 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive"
          >
            {error}
          </p>
        )}
        {pane === "plan" &&
          (task ? (
            <div className="space-y-3">
              {requirement && (
                <div className="space-y-2 py-1">
                  <p className="text-xs text-muted-foreground">
                    {t("linkedRequirement")}
                  </p>
                  <p className="font-medium">{requirement.title}</p>
                  {requirement.description && (
                    <p className="text-xs text-muted-foreground">
                      {requirement.description}
                    </p>
                  )}
                </div>
              )}
              <div className="space-y-2 py-1">
                <p className="text-xs text-muted-foreground">{t("criteria")}</p>
                <p className="whitespace-pre-wrap">
                  {task.checks || t("noCriteria")}
                </p>
              </div>
              <div className="space-y-2 py-1">
                <p className="text-xs text-muted-foreground">
                  {t("pinnedRevision")}
                </p>
                <p className="font-mono text-xs">
                  {task.reference || t("noRevision")}
                </p>
              </div>
            </div>
          ) : (
            <p className="text-muted-foreground">{t("selectTask")}</p>
          ))}
        {pane === "diff" &&
          pending(t("noChangeReview"), t("reviewInWorkspace"))}
        {pane === "files" &&
          (project ? (
            <div className="space-y-2">
              <p className="font-mono text-xs text-muted-foreground">
                {project.root}
              </p>
              {pending(t("noFileTree"), t("browseWorkspace"))}
            </div>
          ) : (
            <p className="text-muted-foreground">{t("pickProject")}</p>
          ))}
        {pane === "terminal" &&
          (project ? (
            <div className="space-y-2">
              <div className="flex gap-2">
                <Button
                  size="xs"
                  variant="outline"
                  disabled={!!busy}
                  onClick={() => loadLog("backend")}
                >
                  {busy === "backend" ? (
                    <LoaderCircle className="motion-safe:animate-spin" />
                  ) : (
                    <Server />
                  )}
                  {t("backendLogs")}
                </Button>
                <Button
                  size="xs"
                  variant="outline"
                  disabled={!!busy}
                  onClick={() => loadLog("frontend")}
                >
                  {busy === "frontend" ? (
                    <LoaderCircle className="motion-safe:animate-spin" />
                  ) : (
                    <Monitor />
                  )}
                  {t("frontendLogs")}
                </Button>
              </div>
              <pre className="max-h-[420px] overflow-auto rounded-lg border bg-muted/40 p-3 text-xs leading-relaxed">
                {log || t("readLogs")}
              </pre>
            </div>
          ) : (
            <p className="text-muted-foreground">{t("pickProject")}</p>
          ))}
        {pane === "preview" &&
          (preview ? (
            <iframe
              title={t("projectPreview")}
              src={preview}
              className="h-[420px] w-full rounded-lg border"
            />
          ) : (
            <p className="text-muted-foreground">{t("noPreview")}</p>
          ))}
        {pane === "diff" && runs.length > 0 && (
          <div className="mt-3 space-y-1">
            <p className="text-xs text-muted-foreground">
              {t("recentInvocations")}
            </p>
            {runs.slice(0, 5).map((row) => (
              <p
                key={row.id}
                className="font-mono text-[11px] text-muted-foreground"
              >
                {t("invocationStatus", {
                  capability: row.capability,
                  status: row.status,
                })}
              </p>
            ))}
          </div>
        )}
      </TabsContent>
    </Tabs>
  );
}
