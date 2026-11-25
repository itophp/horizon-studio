import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useCallback, useEffect, useRef, useState } from "react";
import { ChevronDown, LoaderCircle, RefreshCw, FilePlus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ApiError } from "@/api/client/request";
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  NativeSelect,
  NativeSelectOption,
} from "@/components/ui/native-select";
import { errorText } from "@/features/identity/shared";
import { studio, type Project, type ProjectService } from "./api";
import { isRunning, isStopped, serviceStatusOptions } from "./service-status";
import { ServiceActions, type ServiceAction } from "./service-actions";

const emptyServices: ProjectService[] = [];
function recordIntent(action: ServiceAction) {
  return { action, at: Date.now() };
}
type Notice = { tone: "info" | "success" | "error"; text: string } | null;
type Failure = { service: string; headline: string; log: string };

const tailLog = (log: string, lines = 6, maxChars = 600) => {
  const tail = log.trim().split("\n").slice(-lines).join("\n");
  return tail.length > maxChars ? `${tail.slice(0, maxChars)}…` : tail;
};
const stateText = (
  services: ProjectService[],
  t: import("i18next").TFunction<"studio">,
) => {
  if (
    !services.some(isRunning) &&
    !(services.length > 0 && services.every(isStopped))
  )
    return {
      label: t("unknownStatus"),
      tone: "bg-amber-500",
      detail: t("statusNotLoaded"),
    };
  const running = services.filter(isRunning).length;
  if (running === services.length)
    return {
      label: t("servicesRunning", { running, total: services.length }),
      tone: "bg-emerald-500",
      detail: t("servicesAllRunning", {
        services: services.map((row) => row.service).join(", "),
      }),
    };
  if (running === 0)
    return {
      label: t("servicesStopped", { total: services.length }),
      tone: "bg-muted-foreground/40",
      detail: t("servicesNoneRunning", {
        services: services.map((row) => row.service).join(", "),
      }),
    };
  return {
    label: t("servicesRunning", { running, total: services.length }),
    tone: "bg-amber-500",
    detail: t("partialStatus"),
  };
};

// Service control cluster. State is read from the project CLI through the
// Gateway, so the header reports what actually runs instead of guessing from the
// click that was issued. Every action is followed by status polling until the
// services settle or a timeout is reported.
export default function RunControls({
  project,
  onCreateRepairTask,
  initialized = true,
}: {
  project?: Project;
  initialized?: boolean;
  onCreateRepairTask?: (draft: { title: string; checks: string }) => void;
}) {
  const { t } = useTranslation("studio");
  const [target, setTarget] = useState("all");
  const [pending, setPending] = useState<ServiceAction | null>(null);
  const [notice, setNotice] = useState<Notice>(null);
  const [failures, setFailures] = useState<Failure[]>([]);
  const timer = useRef<number | undefined>(undefined);
  // Deliberate intent vs. observed reality: only a stop the operator asked for
  // counts as "手动停止"; anything else that ends up not running is a fault.
  const intent = useRef<{ action: ServiceAction | "status"; at: number }>({
    action: "status",
    at: 0,
  });
  const seenRunning = useRef<Record<string, boolean>>({});

  // Fault = a service that was seen running (or asked to start) and is not
  // running now, unless the operator's own stop explains it.
  const faultsOf = useCallback((list: ProjectService[]) => {
    const manualStop =
      intent.current.action === "stop" &&
      Date.now() - intent.current.at < 120_000;
    if (manualStop) return [];
    return list.filter(
      (row) => !isRunning(row) && seenRunning.current[row.service] === true,
    );
  }, []);

  const status = useQuery(serviceStatusOptions(project?.id, target));
  const services = status.data ?? emptyServices;
  const read = async () =>
    (await status.refetch({ throwOnError: true })).data ?? emptyServices;
  useEffect(() => () => window.clearTimeout(timer.current), []);

  useEffect(() => {
    if (!project) return;
    for (const row of services) {
      if (isRunning(row)) seenRunning.current[row.service] = true;
    }
    const faults = faultsOf(services);
    let live = true;
    void Promise.all(
      faults.map(async (row) => {
        const log = await studio.log(project.id, row.service).catch(() => "");
        return {
          service: row.service,
          headline: t("stoppedUnexpectedly"),
          log: tailLog(log),
        };
      }),
    ).then((rows) => {
      if (live) setFailures(rows);
    });
    return () => {
      live = false;
    };
  }, [faultsOf, services, project, t]);

  const activate = async (action: ServiceAction) => {
    if (!project || pending || (action !== "stop" && !initialized)) return;
    intent.current = recordIntent(action);
    setPending(action);
    setNotice({
      tone: "info",
      text:
        action === "start"
          ? t("starting")
          : action === "restart"
            ? t("restarting")
            : t("stopping"),
    });
    try {
      await studio.control(project.id, action, target);
      const label =
        action === "start"
          ? t("started")
          : action === "restart"
            ? t("restarted")
            : t("stopped");
      const want = action !== "stop";
      for (let attempt = 0; attempt < 15; attempt++) {
        const list = await read();
        const settled =
          list.length > 0 && list.every(want ? isRunning : isStopped);
        if (settled) {
          const faults = faultsOf(list);
          setNotice(
            want && faults.length > 0
              ? {
                  tone: "error",
                  text: t("didNotStayRunning", {
                    label,
                    services: faults.map((row) => row.service).join("、"),
                  }),
                }
              : {
                  tone: "success",
                  text: t("operationResult", {
                    label,
                    services: list
                      .map((row) => `${row.service}(${row.status})`)
                      .join("、"),
                  }),
                },
          );
          setPending(null);
          return;
        }
        await new Promise((resolve) => {
          timer.current = window.setTimeout(resolve, 2000);
        });
      }
      const list = await read();
      setNotice({
        tone: "error",
        text: t("operationTimeout", {
          services: list
            .map((row) => `${row.service}=${row.status}`)
            .join(", "),
        }),
      });
      if (want) {
        // A start that never reached "running" is a start failure, whether or
        // not the service had run before in this browser session.
        const stopped = list.filter((row) => !isRunning(row));
        const details = await Promise.all(
          stopped.map(async (row) => {
            const log = await studio
              .log(project.id, row.service)
              .catch(() => "");
            return {
              service: row.service,
              headline: t("startupFailed"),
              log: tailLog(log),
            };
          }),
        );
        if (details.length > 0) setFailures(details);
      }
    } catch (error) {
      setNotice({
        tone: "error",
        text:
          error instanceof ApiError && error.code === "initialization_required"
            ? t("initializationRequired")
            : errorText(error, t),
      });
      void status.refetch();
    } finally {
      setPending(null);
    }
  };

  if (!project) return null;
  const confirmedServices = status.isError ? emptyServices : services;
  const summary = stateText(confirmedServices, t);
  const runningServices = confirmedServices.filter(isRunning);
  const canStart =
    confirmedServices.length > 0 && confirmedServices.every(isStopped);

  const displayNotice =
    notice ??
    (status.isError
      ? { tone: "error", text: errorText(status.error, t) }
      : null);
  return (
    <>
      <ServiceActions
        runningServices={runningServices}
        canStart={canStart}
        initialized={initialized}
        pending={pending}
        activate={activate}
      />
      <span role="status" className="sr-only">
        {displayNotice?.text}
      </span>
      <Popover>
        <PopoverTrigger
          render={
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="rounded-full text-muted-foreground"
            />
          }
          title={displayNotice?.text ?? summary.detail}
        >
          {pending ? (
            <LoaderCircle
              className="size-3.5 motion-safe:animate-spin"
              aria-hidden="true"
            />
          ) : (
            <span
              className={`size-1.5 rounded-full ${summary.tone}`}
              aria-hidden="true"
            />
          )}
          <span>{summary.label}</span>
          <ChevronDown className="size-3 text-muted-foreground" />
        </PopoverTrigger>
        <PopoverContent
          align="end"
          className="w-[min(22.5rem,calc(100vw-1rem))] max-h-[75dvh] overflow-auto p-3"
        >
          <div className="mb-2 flex items-center justify-between gap-2">
            <PopoverTitle>{t("devServices")}</PopoverTitle>
            <div className="flex items-center gap-1.5">
              <NativeSelect
                value={target}
                disabled={pending !== null}
                onChange={(event) => {
                  setTarget(event.target.value);
                  setNotice(null);
                }}
                className="h-7 text-xs"
                aria-label={t("devServices")}
              >
                <NativeSelectOption value="all">{t("all")}</NativeSelectOption>
                <NativeSelectOption value="backend">
                  {t("backend")}
                </NativeSelectOption>
                <NativeSelectOption value="frontend">
                  {t("frontend")}
                </NativeSelectOption>
              </NativeSelect>
              <Button
                size="xs"
                variant="ghost"
                onClick={() =>
                  void read().catch((error) =>
                    setNotice({ tone: "error", text: errorText(error, t) }),
                  )
                }
              >
                <RefreshCw size={12} /> {t("refresh")}
              </Button>
            </div>
          </div>
          {displayNotice && (
            <p
              className={`rounded-md px-2 py-1 text-xs [overflow-wrap:anywhere] ${displayNotice.tone === "error" ? "bg-destructive/10 text-destructive" : "bg-muted text-muted-foreground"}`}
            >
              {displayNotice.text}
            </p>
          )}
          <div className="space-y-1">
            {services.length === 0 && (
              <p className="py-3 text-center text-xs text-muted-foreground">
                {t("noServiceStatus")}
              </p>
            )}
            {services.map((row) => (
              <div
                key={row.service}
                className="flex items-center justify-between gap-2 rounded-md border px-2 py-1.5"
              >
                <span className="flex items-center gap-1.5 text-xs">
                  <span
                    className={`size-1.5 rounded-full ${isRunning(row) ? "bg-emerald-500" : "bg-muted-foreground/40"}`}
                  />
                  {row.service}
                  {row.pid ? (
                    <span className="text-muted-foreground">
                      {t("servicePid", { pid: row.pid })}
                    </span>
                  ) : null}
                </span>
                <span
                  className={`text-[11px] ${isRunning(row) ? "text-emerald-700" : "text-muted-foreground"}`}
                >
                  {row.status}
                </span>
              </div>
            ))}
          </div>
          {services[0]?.log && (
            <p
              className="mt-2 truncate font-mono text-[10px] text-muted-foreground"
              title={services[0].log}
            >
              {t("logPath", { path: services[0].log })}
            </p>
          )}
          {failures.length > 0 && (
            <div className="mt-3 space-y-2 rounded-md border border-destructive/30 bg-destructive/5 p-2">
              <p className="text-[11px] font-medium text-destructive">
                {failures
                  .map((row) => `${row.service} ${row.headline}`)
                  .join("；")}
              </p>
              {failures.map((row) => (
                <pre
                  key={row.service}
                  className="max-h-28 overflow-auto rounded bg-card p-2 text-[10px] leading-relaxed"
                >
                  {row.log || t("emptyLogs")}
                </pre>
              ))}
              {onCreateRepairTask && (
                <Button
                  size="xs"
                  variant="outline"
                  onClick={() => {
                    const title = t("repairTitle", {
                      services: failures
                        .map((row) => `${row.service} ${row.headline}`)
                        .join("; "),
                    });
                    const checks = [
                      t("repairSymptoms", {
                        services: failures
                          .map((row) => `${row.service} ${row.headline}`)
                          .join("; "),
                      }),
                      t("recentLogs"),
                      ...failures.map(
                        (row) =>
                          `【${row.service}】\n${row.log || t("emptyLogs")}`,
                      ),
                      "",
                      t("repairCriteria"),
                    ].join("\n");
                    onCreateRepairTask({ title, checks });
                  }}
                >
                  <FilePlus />
                  {t("repairTask")}
                </Button>
              )}
            </div>
          )}
        </PopoverContent>
      </Popover>
    </>
  );
}
