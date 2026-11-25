import type { SessionProfile } from "@/api/generated/models";
import { mcpManagementCreate } from "@/lib/capabilities";
import { agentOrigin, getAgentConnection, PROTOCOL } from "./agent-connection";

type Identity = {
  accountId: number;
  tenantId: string;
  identityBindingId: string;
};

export function isStudioPairingAvailable() {
  return import.meta.env.DEV && window.location.hostname === "127.0.0.1";
}

export function studioMcpFrameURL(value: string) {
  const url = new URL(value);
  if (url.origin !== agentOrigin) throw new Error("invalid_dsh_origin");
  url.hash = "horizonstudio-mcp";
  return url.href;
}

export function probeStudioMcp(frame: Window | null | undefined) {
  frame?.postMessage({ protocol: PROTOCOL, op: "mcp-pair-probe" }, agentOrigin);
}

export function isStudioMcpReady(
  event: MessageEvent,
  frame: Window | null | undefined,
) {
  return Boolean(
    frame &&
    event.source === frame &&
    event.origin === agentOrigin &&
    event.data?.protocol === PROTOCOL &&
    event.data.event === "mcp-pair-ready",
  );
}

export async function issueStudioMcpCredential(
  password: string,
  signal: AbortSignal,
) {
  const issued = await mcpManagementCreate(
    { name: "HorizonStudio DSH", days: 30, password },
    signal,
  );
  signal.throwIfAborted();
  if (issued.status !== 200 || !issued.data.secret)
    throw new Error("credential_issue_failed");
  return issued.data.secret;
}

function requestStudioMcpPairing(
  frame: Window,
  op: "mcp-pair" | "mcp-pair-check",
  credential: Identity & { token?: string },
  requestSignal: AbortSignal,
) {
  const requestId = crypto.randomUUID();
  return new Promise<boolean>((resolve, reject) => {
    const signal = AbortSignal.any([requestSignal, AbortSignal.timeout(35000)]);
    signal.throwIfAborted();
    signal.addEventListener("abort", () => reject(signal.reason), {
      once: true,
    });
    window.addEventListener(
      "message",
      (reply: MessageEvent) => {
        if (
          reply.origin !== agentOrigin ||
          reply.source !== frame ||
          reply.data?.protocol !== PROTOCOL ||
          reply.data.event !== "mcp-pair-result" ||
          reply.data.requestId !== requestId
        )
          return;
        if (reply.data.ok === true) resolve(reply.data.connected === true);
        else reject(new Error("pairing_failed"));
      },
      { signal },
    );
    frame.postMessage(
      { protocol: PROTOCOL, op, requestId, credential },
      agentOrigin,
    );
  });
}

export async function saveStudioMcpCredential(
  frame: Window,
  credential: Identity & { token: string },
  signal: AbortSignal,
) {
  await requestStudioMcpPairing(frame, "mcp-pair", credential, signal);
}

export async function pairStudioMcpAfterLogin(
  profile: SessionProfile,
  password: string,
  isCurrentSession: () => boolean,
) {
  if (!profile.account || !profile.permissions.includes("studio:read"))
    throw new Error("studio_access_required");
  const lifetime = new AbortController();
  const readySignal = AbortSignal.any([
    lifetime.signal,
    AbortSignal.timeout(12000),
  ]);
  const frame = document.createElement("iframe");
  frame.hidden = true;
  try {
    const { data } = await getAgentConnection(readySignal);
    readySignal.throwIfAborted();
    const url = studioMcpFrameURL(data.url);
    const ready = new Promise<void>((resolve, reject) => {
      readySignal.addEventListener("abort", () => reject(readySignal.reason), {
        once: true,
      });
      window.addEventListener(
        "message",
        (event) => {
          if (isStudioMcpReady(event, frame.contentWindow)) resolve();
        },
        { signal: readySignal },
      );
    });
    frame.addEventListener("load", () => probeStudioMcp(frame.contentWindow), {
      signal: lifetime.signal,
    });
    frame.src = url;
    document.body.append(frame);
    await ready;
    if (!isCurrentSession()) throw new Error("session_changed");
    const target = frame.contentWindow;
    if (!target) throw new Error("dsh_unavailable");
    const identity = {
      accountId: profile.account.id,
      tenantId: profile.tenantId,
      identityBindingId: profile.identityBindingId ?? "",
    };
    const checkSignal = AbortSignal.any([
      lifetime.signal,
      AbortSignal.timeout(12000),
    ]);
    if (
      await requestStudioMcpPairing(
        target,
        "mcp-pair-check",
        identity,
        checkSignal,
      )
    )
      return;
    if (!isCurrentSession()) throw new Error("session_changed");
    const signal = AbortSignal.any([
      lifetime.signal,
      AbortSignal.timeout(35000),
    ]);
    const token = await issueStudioMcpCredential(password, signal);
    if (!isCurrentSession()) throw new Error("session_changed");
    await saveStudioMcpCredential(target, { ...identity, token }, signal);
  } finally {
    lifetime.abort();
    frame.remove();
  }
}
