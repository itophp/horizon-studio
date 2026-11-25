import { useLayoutEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Activity, Pause, Play, Terminal } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ErrorBanner, Loading } from "@/features/identity/shared";
import { studio, type ProjectService } from "./api";
import { Library, RecordHeader } from "./library";
import {
  isRunning,
  isStopped,
  runningServiceURL,
  serviceStatusOptions,
} from "./service-status";

const serviceNames = ["backend", "frontend"] as const;
type ServiceName = (typeof serviceNames)[number];

function statusLabel(service?: ProjectService) {
  if (service?.reason === "process_exited") return "runExited";
  if (service && isRunning(service)) return "runRunning";
  if (service && isStopped(service)) return "runStopped";
  return "unknownStatus";
}

function formatUptime(seconds: number, locale?: string) {
  const parts = [
    ["hour", Math.floor(seconds / 3600)],
    ["minute", Math.floor((seconds % 3600) / 60)],
  ] as const;
  const displayed = parts
    .filter(([, value]) => value > 0)
    .map(([unit, value]) =>
      new Intl.NumberFormat(locale, {
        style: "unit",
        unit,
        unitDisplay: "short",
      }).format(value),
    );
  if (!displayed.length) {
    displayed.push(
      new Intl.NumberFormat(locale, {
        style: "unit",
        unit: "second",
        unitDisplay: "short",
      }).format(seconds),
    );
  }
  return new Intl.ListFormat(locale, { style: "short", type: "unit" }).format(
    displayed,
  );
}

function LogOutput({ log }: { log: string }) {
  const output = useRef<HTMLPreElement>(null);
  const follow = useRef(true);
  useLayoutEffect(() => {
    const node = output.current;
    if (node && follow.current) node.scrollTop = node.scrollHeight;
  }, [log]);
  return (
    <pre
      ref={output}
      tabIndex={0}
      className="min-h-44 max-h-[55dvh] overflow-auto rounded-lg bg-muted/50 p-4 font-mono text-xs leading-relaxed whitespace-pre-wrap [overflow-wrap:anywhere] focus-visible:outline-ring"
      onScroll={(event) => {
        const node = event.currentTarget;
        follow.current =
          node.scrollHeight - node.scrollTop - node.clientHeight < 24;
      }}
    >
      {log}
    </pre>
  );
}

export function RunOverview({
  project,
  selected,
  onSelect,
}: {
  project: number;
  selected?: string;
  onSelect: (service: ServiceName) => void;
}) {
  const { t, i18n } = useTranslation("studio");
  const active: ServiceName = selected === "frontend" ? "frontend" : "backend";
  const [live, setLive] = useState(true);
  const status = useQuery(serviceStatusOptions(project));
  const logs = useQuery({
    queryKey: ["studio-service-log", project, active],
    queryFn: ({ signal }) => studio.log(project, active, signal),
    refetchInterval: live ? 4000 : false,
    refetchOnWindowFocus: live,
    refetchOnReconnect: live,
    retry: false,
  });
  const services = status.isError ? [] : (status.data ?? []);
  const service = services.find((row) => row.service === active);
  const url = runningServiceURL(
    services,
    active,
    active === "backend" ? "/api/health" : "/",
  );
  const locale = i18n.resolvedLanguage;
  const formatTime = (value: string | number) => {
    const date = new Date(value);
    return Number.isNaN(date.getTime())
      ? t("unknownStatus")
      : new Intl.DateTimeFormat(locale, {
          dateStyle: "medium",
          timeStyle: "medium",
        }).format(date);
  };
  const startedAt = service?.startedAt ? Date.parse(service.startedAt) : NaN;
  const uptime =
    service && isRunning(service) && Number.isFinite(startedAt)
      ? Math.max(0, Math.floor((status.dataUpdatedAt - startedAt) / 1000))
      : undefined;
  const log = logs.data ?? "";
  const latestError = log
    .split(/\r?\n/)
    .reverse()
    .find((line) =>
      /\b(?:error|fatal|failed|eaddrinuse|econnrefused)\b|panic:/i.test(line),
    );
  const reason = service?.reason === "process_exited";
  const refresh = () => {
    void status.refetch();
    void logs.refetch();
  };

  return (
    <Library
      entries={serviceNames.map((name) => ({
        id: name,
        title: t(name),
        subtitle: t(statusLabel(services.find((row) => row.service === name))),
      }))}
      selected={active}
      onSelect={(id) => onSelect(id === "frontend" ? "frontend" : "backend")}
      heading={t("run")}
      fetching={status.isFetching || logs.isFetching}
      refresh={refresh}
    >
      <RecordHeader
        title={t(active)}
        subtitle={t("devServices")}
        action={
          <Badge variant={reason ? "destructive" : "secondary"}>
            <span
              className={`size-1.5 rounded-full ${service && isRunning(service) ? "bg-emerald-500" : reason ? "bg-destructive" : "bg-muted-foreground/50"}`}
              aria-hidden="true"
            />
            {t(statusLabel(service))}
          </Badge>
        }
      />
      <div className="min-w-0 space-y-6 px-5 py-3 sm:px-7">
        {status.isPending && <Loading />}
        {status.isError && (
          <ErrorBanner
            error={status.error}
            onRetry={() => void status.refetch()}
          />
        )}
        <dl className="flex flex-wrap gap-x-8 gap-y-3 text-sm">
          {url && (
            <div className="space-y-1">
              <dt className="text-xs text-muted-foreground">
                {t(active === "backend" ? "projectAPI" : "projectWeb")}
              </dt>
              <dd>
                <a
                  href={url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="font-mono underline underline-offset-2"
                >
                  {new URL(url).origin}
                </a>
              </dd>
            </div>
          )}
          {service && (
            <div className="space-y-1">
              <dt className="text-xs text-muted-foreground">
                {t("runStartedAt")}
              </dt>
              <dd className="tabular-nums">
                {service.startedAt
                  ? formatTime(service.startedAt)
                  : t("runStartUnavailable")}
              </dd>
            </div>
          )}
          {uptime !== undefined && (
            <div className="space-y-1">
              <dt className="text-xs text-muted-foreground">
                {t("runUptime")}
              </dt>
              <dd className="tabular-nums">{formatUptime(uptime, locale)}</dd>
            </div>
          )}
          {service?.pid && (
            <div className="space-y-1">
              <dt className="text-xs text-muted-foreground">
                {t("runProcessId")}
              </dt>
              <dd className="font-mono">{service.pid}</dd>
            </div>
          )}
          {status.dataUpdatedAt > 0 && !status.isError && (
            <div className="space-y-1">
              <dt className="text-xs text-muted-foreground">
                {t("runCheckedAt")}
              </dt>
              <dd className="tabular-nums">
                {formatTime(status.dataUpdatedAt)}
              </dd>
            </div>
          )}
        </dl>
        {(reason || latestError) && (
          <section className="space-y-2 rounded-lg bg-destructive/5 p-3">
            <h3 className="flex items-center gap-2 text-sm font-semibold">
              <Activity className="size-4 text-destructive" />
              {t("runDiagnostics")}
            </h3>
            {reason && <p className="text-sm">{t("runExitReason")}</p>}
            {latestError && (
              <p className="font-mono text-xs text-muted-foreground [overflow-wrap:anywhere]">
                {latestError.trim().slice(0, 500)}
              </p>
            )}
          </section>
        )}
        <section className="min-w-0 space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="min-w-0">
              <h3 className="flex items-center gap-2 text-sm font-semibold">
                <Terminal className="size-4 text-muted-foreground" />
                {t("runLiveLogs")}
              </h3>
              {logs.dataUpdatedAt > 0 && (
                <p className="mt-1 text-xs text-muted-foreground">
                  {t("runLogUpdated", { time: formatTime(logs.dataUpdatedAt) })}
                </p>
              )}
            </div>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setLive((current) => !current)}
            >
              {live ? <Pause /> : <Play />}
              {t(live ? "runPause" : "runResume")}
            </Button>
          </div>
          {logs.isPending && <Loading />}
          {logs.isError && (
            <ErrorBanner
              error={logs.error}
              onRetry={() => void logs.refetch()}
            />
          )}
          {log && <LogOutput key={active} log={log} />}
          {logs.isSuccess && !log && (
            <p className="rounded-lg bg-muted/50 px-4 py-6 text-sm text-muted-foreground">
              {t("emptyLogs")}
            </p>
          )}
        </section>
      </div>
    </Library>
  );
}
