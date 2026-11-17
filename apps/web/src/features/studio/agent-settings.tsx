import { useEffect, useRef, useState } from "react";
import { LoaderCircle, Settings } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { agentOrigin, PROTOCOL, useAgentConnection } from "./agent-connection";

export function AgentSettings({ sessionScope }: { sessionScope: string }) {
  const { t } = useTranslation("studio");
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState<"loading" | "ready" | "error">(
    "loading",
  );
  const frame = useRef<HTMLIFrameElement>(null);
  const connection = useAgentConnection(sessionScope, open);
  const url = connection.data?.data.url;
  const failed = connection.isError || status === "error";

  useEffect(() => {
    if (!open || !url) return;
    const timeout = window.setTimeout(() => setStatus("error"), 15000);
    const onMessage = (event: MessageEvent) => {
      if (
        event.origin !== agentOrigin ||
        event.source !== frame.current?.contentWindow ||
        event.data?.protocol !== PROTOCOL
      )
        return;
      if (event.data.event === "settings-opened") {
        window.clearTimeout(timeout);
        setStatus("ready");
      }
      if (event.data.event === "settings-closed") setOpen(false);
    };
    window.addEventListener("message", onMessage);
    return () => {
      window.clearTimeout(timeout);
      window.removeEventListener("message", onMessage);
    };
  }, [open, url]);

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) setStatus("loading");
        setOpen(next);
      }}
    >
      <DialogTrigger
        render={<Button type="button" variant="ghost" size="icon" />}
        className="studio-nav-settings size-9 text-muted-foreground"
        aria-label={t("agentSettings")}
        title={t("agentSettings")}
      >
        <Settings className="size-[18px]" aria-hidden="true" />
      </DialogTrigger>
      <DialogContent
        className={
          status === "ready"
            ? "h-dvh max-w-none rounded-none bg-transparent p-0 ring-0 sm:max-w-none"
            : "content-center"
        }
        showCloseButton={status !== "ready"}
      >
        <DialogTitle className="sr-only">{t("agentSettings")}</DialogTitle>
        {open && url && !failed && (
          <iframe
            ref={frame}
            title={t("agentSettings")}
            src={`${url}#horizonstudio-settings`}
            className={
              status === "ready"
                ? "size-full border-0"
                : "pointer-events-none absolute h-dvh w-dvw opacity-0"
            }
            tabIndex={status === "ready" ? 0 : -1}
            aria-hidden={status !== "ready"}
          />
        )}
        {failed ? (
          <p role="alert" className="py-6 text-center">
            {t("agentSettingsUnavailable")}
          </p>
        ) : (
          status === "loading" && (
            <p
              role="status"
              className="flex items-center justify-center gap-2 py-6"
            >
              <LoaderCircle
                className="size-4 motion-safe:animate-spin"
                aria-hidden="true"
              />
              {t("agentSettingsLoading")}
            </p>
          )
        )}
      </DialogContent>
    </Dialog>
  );
}
