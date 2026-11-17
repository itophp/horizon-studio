import { useTranslation } from "react-i18next";
import {
  useCallback,
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { LoaderCircle, Sparkles } from "lucide-react";
import { studio, type Item, type Requirement } from "./api";

import {
  agentOrigin,
  PROTOCOL,
  useAgentConnection,
  type AgentSessionStatus,
} from "./agent-connection";

// The conversation is DSH's own session page. The shell only guarantees the
// binding: one task ↔ one DSH Session. Full task context stays in Studio.
export default function Session({
  task,
  requirement,
  projectRoot,
  canWrite,
  sessionScope,
  active,
  onAgentStatus,
}: {
  task?: Item;
  requirement?: Requirement;
  projectRoot?: string;
  canWrite: boolean;
  sessionScope: string;
  active: boolean;
  onAgentStatus?: (statuses: AgentSessionStatus[]) => void;
}) {
  const { t } = useTranslation("studio");
  const frame = useRef<HTMLIFrameElement>(null);
  const connection = useAgentConnection(sessionScope, active);
  const url = connection.data?.data.url;
  const taskId = task?.id;
  const selection = useMemo(() => ({ taskId, url }), [taskId, url]);
  const [result, setResult] = useState<{
    selection: typeof selection;
    sessionId?: string;
    failed?: boolean;
  }>();
  const [readyUrl, setReadyUrl] = useState<string>();
  const [timedOutUrl, setTimedOutUrl] = useState<string>();
  const creating = useRef<Promise<string> | undefined>(undefined);
  const confirmedBindings = useRef(new Map<number, string>());
  const ready = Boolean(url && readyUrl === url);
  const currentResult = result?.selection === selection ? result : undefined;
  const bound = currentResult?.sessionId;
  const error = currentResult?.failed
    ? t("agentError")
    : !ready && url && timedOutUrl === url
      ? t("agentTimeout")
      : "";
  const pending = Boolean(task && url && !bound && !error);
  const emitStatuses = useEffectEvent((statuses: AgentSessionStatus[]) => {
    onAgentStatus?.(statuses);
  });

  useEffect(() => {
    if (!active || !url || ready) return;
    const timeout = window.setTimeout(() => setTimedOutUrl(url), 15000);
    return () => window.clearTimeout(timeout);
  }, [active, url, ready]);

  // The DSH surface announces itself once its bridge plugin is applied; talking
  // to it earlier would drop the request into a listener that does not exist yet.
  useLayoutEffect(() => {
    const onMessage = (event: MessageEvent) => {
      if (
        event.origin !== agentOrigin ||
        event.source !== frame.current?.contentWindow
      )
        return;
      const data = event.data;
      if (data?.protocol !== PROTOCOL) return;
      if (data.event === "ready") setReadyUrl(url);
      if (data.event === "session-status" && Array.isArray(data.statuses))
        emitStatuses(data.statuses as AgentSessionStatus[]);
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [url]);

  const call = useCallback(
    (op: string, extra: Record<string, unknown> = {}) =>
      new Promise<{ sessionId?: string; statuses?: AgentSessionStatus[] }>(
        (resolve, reject) => {
          const requestId = crypto.randomUUID();
          const target = frame.current?.contentWindow;
          if (!target) return reject(new Error(t("sessionLoading")));
          const onMessage = (event: MessageEvent) => {
            if (
              event.origin !== agentOrigin ||
              event.source !== frame.current?.contentWindow
            )
              return;
            const data = event.data;
            if (
              !data ||
              data.protocol !== PROTOCOL ||
              data.requestId !== requestId
            )
              return;
            window.clearTimeout(timeout);
            window.removeEventListener("message", onMessage);
            if (data.ok) resolve(data);
            else reject(new Error(data.message ?? t("agentError")));
          };
          window.addEventListener("message", onMessage);
          const timeout = window.setTimeout(() => {
            window.removeEventListener("message", onMessage);
            reject(new Error(t("agentTimeout")));
          }, 15000);
          target.postMessage(
            { protocol: PROTOCOL, requestId, op, ...extra },
            agentOrigin,
          );
        },
      ),
    [t],
  );

  const openSelection = useEffectEvent(async (signal: AbortSignal) => {
    if (!task) return;
    const bindingKey = `horizonstudio:task-session:${sessionScope}:${task.id}`;
    try {
      // A new session must bind before another task takes over the composer.
      while (creating.current) {
        await creating.current.catch(() => undefined);
        if (signal.aborted) return;
      }
      const persistedId =
        task.dshSessionId ?? confirmedBindings.current.get(task.id);
      const cachedId = localStorage.getItem(bindingKey);
      let sessionId = persistedId ?? cachedId;
      if (canWrite && !persistedId) {
        creating.current = (async () => {
          const candidate =
            cachedId ??
            (
              await call("create", {
                projectRoot,
                prompt: task.title,
              })
            ).sessionId;
          if (!candidate) throw new Error(t("agentError"));
          // Keep the candidate if saving the binding needs to be retried.
          localStorage.setItem(bindingKey, candidate);
          const linked = await studio.linkSession(task.id, candidate);
          confirmedBindings.current.set(task.id, linked.sessionId);
          localStorage.setItem(bindingKey, linked.sessionId);
          return linked.sessionId;
        })().finally(() => {
          creating.current = undefined;
        });
        sessionId = await creating.current;
      }
      if (signal.aborted) return;
      if (!sessionId) throw new Error(t("agentError"));
      const opened = await call("open", { sessionId });
      if (opened.sessionId !== sessionId) throw new Error(t("agentError"));
      if (!signal.aborted) setResult({ selection, sessionId });
    } catch {
      if (!signal.aborted) setResult({ selection, failed: true });
    }
  });

  useEffect(() => {
    if (!active || !ready || selection.taskId === undefined) return;
    const request = new AbortController();
    void openSelection(request.signal);
    return () => request.abort();
  }, [active, ready, selection]);

  // Push-only updates can arrive before this frame is listening; ask once the
  // bridge announces ready so every known Session gets a baseline.
  useEffect(() => {
    if (!active || !ready) return;
    let cancelled = false;
    void call("statuses")
      .then((result) => {
        if (!cancelled && Array.isArray(result.statuses))
          emitStatuses(result.statuses);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [active, call, ready]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {(pending || error || bound) && (
        <div className="flex shrink-0 items-center gap-2 px-4 py-1.5 text-[11px] text-muted-foreground">
          <Sparkles size={12} className="text-primary" />
          {pending && (
            <>
              <LoaderCircle size={11} className="animate-spin" />{" "}
              {t("sessionLoading")}
            </>
          )}
          {bound && (
            <span>
              {requirement
                ? t("boundRequirement", { title: requirement.title })
                : t("bound")}
            </span>
          )}
          {error && <span className="text-destructive">{error}</span>}
        </div>
      )}
      <div className="relative min-h-0 flex-1">
        {!task && (
          <p className="absolute inset-0 grid place-items-center p-6 text-sm text-muted-foreground">
            {t("selectSession")}
          </p>
        )}
        {task && connection.isLoading ? (
          <div
            role="status"
            className="grid h-full place-items-center p-6 text-sm text-muted-foreground"
          >
            <span className="flex items-center gap-2">
              <LoaderCircle className="size-4 motion-safe:animate-spin" />
              {t("sessionLoading")}
            </span>
          </div>
        ) : url ? (
          <iframe
            key={url}
            ref={frame}
            title={t("sessionTitle")}
            src={url}
            className={
              !bound ? "invisible size-full border-0" : "size-full border-0"
            }
            onLoad={() =>
              frame.current?.contentWindow?.postMessage(
                { protocol: PROTOCOL, op: "probe" },
                agentOrigin,
              )
            }
          />
        ) : task ? (
          <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center">
            <Sparkles size={22} className="text-primary" />
            <p className="text-xs text-muted-foreground">
              {t("agentUnavailable")}
            </p>
          </div>
        ) : null}
      </div>
    </div>
  );
}
