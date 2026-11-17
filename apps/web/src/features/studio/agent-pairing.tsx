import { useEffect, useRef, useState, type FormEvent } from "react";
import { Plug, ShieldCheck } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ApiError } from "@/api/client/request";
import { useAgentConnection } from "./agent-connection";
import {
  isStudioMcpReady,
  issueStudioMcpCredential,
  probeStudioMcp,
  saveStudioMcpCredential,
  studioMcpFrameURL,
} from "./studio-mcp-pairing";

export function AgentPairing(props: {
  sessionScope: string;
  accountId: number;
  departmentId: number;
}) {
  const { t } = useTranslation("studio");
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!busy) setOpen(next);
      }}
    >
      <DialogTrigger
        render={<Button type="button" size="sm" variant="ghost" />}
      >
        <Plug aria-hidden />
        {t("pairMcp")}
      </DialogTrigger>
      {open && <PairingForm {...props} onBusyChange={setBusy} />}
    </Dialog>
  );
}

function PairingForm({
  sessionScope,
  accountId,
  departmentId,
  onBusyChange,
}: {
  sessionScope: string;
  accountId: number;
  departmentId: number;
  onBusyChange: (busy: boolean) => void;
}) {
  const { t } = useTranslation("studio");
  const target = useRef<HTMLIFrameElement>(null);
  const secret = useRef<string | undefined>(undefined);
  const lifetime = useRef<AbortController | undefined>(undefined);
  const connection = useAgentConnection(sessionScope, true);
  const url = connection.data?.data.url;
  const [ready, setReady] = useState(false);
  const [password, setPassword] = useState("");
  const [state, setState] = useState<
    "idle" | "busy" | "saved" | "retry" | "error" | "verifyError"
  >("idle");
  const [timedOut, setTimedOut] = useState(false);
  const busy = state === "busy";
  const retry = state === "retry";

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      if (isStudioMcpReady(event, target.current?.contentWindow)) {
        setReady(true);
      }
    };
    const timeout = window.setTimeout(() => setTimedOut(true), 15000);
    window.addEventListener("message", onMessage);
    return () => {
      window.clearTimeout(timeout);
      window.removeEventListener("message", onMessage);
      lifetime.current?.abort();
      secret.current = undefined;
    };
  }, []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const frame = target.current?.contentWindow;
    if (!ready || !frame || busy || lifetime.current) return;
    if (!secret.current && !password) return;
    const request = new AbortController();
    lifetime.current = request;
    setState("busy");
    onBusyChange(true);
    setPassword("");
    try {
      if (!secret.current) {
        secret.current = await issueStudioMcpCredential(
          departmentId,
          password,
          request.signal,
        );
      }
      await saveStudioMcpCredential(
        frame,
        { token: secret.current, accountId, departmentId },
        request.signal,
      );
      secret.current = undefined;
      setState("saved");
    } catch (error) {
      if (!request.signal.aborted)
        setState(
          secret.current
            ? "retry"
            : error instanceof ApiError && error.code === "forbidden"
              ? "verifyError"
              : "error",
        );
    } finally {
      request.abort();
      lifetime.current = undefined;
      onBusyChange(false);
    }
  }

  return (
    <DialogContent showCloseButton={!busy}>
      <DialogHeader>
        <DialogTitle>{t("pairMcp")}</DialogTitle>
        <DialogDescription>{t("pairMcpDescription")}</DialogDescription>
      </DialogHeader>
      {url && (
        <iframe
          ref={target}
          title={t("pairMcp")}
          src={studioMcpFrameURL(url)}
          className="hidden"
          onLoad={() => probeStudioMcp(target.current?.contentWindow)}
        />
      )}
      {state === "saved" ? (
        <p role="status" className="text-sm">
          {t("pairMcpSaved")}
        </p>
      ) : (
        <form onSubmit={(event) => void submit(event)} className="space-y-4">
          {!retry && (
            <div className="space-y-2">
              <Label htmlFor="studio-pair-password">
                {t("pairMcpPassword")}
              </Label>
              <Input
                id="studio-pair-password"
                name="password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                required
                maxLength={128}
                disabled={busy}
              />
            </div>
          )}
          {state === "retry" ||
          state === "error" ||
          state === "verifyError" ||
          (!ready && (timedOut || connection.isError)) ? (
            <p role="alert" className="text-sm text-destructive">
              {t(
                state === "verifyError"
                  ? "pairMcpVerifyFailed"
                  : "pairMcpFailed",
              )}
            </p>
          ) : null}
          <DialogFooter>
            <Button type="submit" disabled={!ready || busy}>
              <ShieldCheck aria-hidden />
              {t(busy ? "pairMcpPending" : "pairMcpConfirm")}
            </Button>
          </DialogFooter>
        </form>
      )}
    </DialogContent>
  );
}
