import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import Session from "./session";
import { SessionWorkspace } from "./session-workspace";
import { agentOrigin, PROTOCOL } from "./agent-connection";
import { studio, type Item } from "./api";

type AgentRequest = {
  op?: string;
  requestId: string;
  sessionId?: string;
  prompt?: string;
};

vi.mock("@/api/client/request", () => ({
  request: vi.fn(async () => ({
    data: { url: "http://127.0.0.1:3095" },
  })),
}));

it("prefills only the task title and persists the created DSH session before showing it", async () => {
  const link = vi
    .spyOn(studio, "linkSession")
    .mockResolvedValue({ sessionId: "created-session" });
  localStorage.removeItem("horizonstudio:task-session:brief-test:11");
  const client = new QueryClient();
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <Session
          task={{
            id: 11,
            title: "Account setup",
            kind: "execution",
            stage: "planned",
          }}
          projectRoot="/workspace/Horizon"
          requirement={{
            id: 3,
            title: "Requirement",
            description: "Private requirement detail",
          }}
          canWrite
          sessionScope="brief-test"
          active
        />
      </QueryClientProvider>
    </I18nextProvider>,
  );

  const frame = (await screen.findByTitle("DSH session")) as HTMLIFrameElement;
  const target = frame.contentWindow!;
  const send = vi.spyOn(target, "postMessage").mockImplementation(() => {});
  const posted = () =>
    send.mock.calls.map(([message]) => message as AgentRequest);
  fireEvent(
    window,
    new MessageEvent("message", {
      origin: "http://127.0.0.1:3095",
      source: target,
      data: { protocol: "horizonstudio/agent", event: "ready" },
    }),
  );
  await waitFor(() =>
    expect(posted()).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ op: "create", prompt: "Account setup" }),
      ]),
    ),
  );
  const create = posted().find(
    (message) => message.op === "create" && message.prompt === "Account setup",
  );
  expect(create).toBeDefined();
  fireEvent(
    window,
    new MessageEvent("message", {
      origin: agentOrigin,
      source: target,
      data: {
        protocol: PROTOCOL,
        requestId: create!.requestId,
        ok: true,
        sessionId: "created-session",
      },
    }),
  );
  await waitFor(() => expect(link).toHaveBeenCalledWith(11, "created-session"));
  expect(posted()).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        op: "open",
        sessionId: "created-session",
      }),
    ]),
  );
  link.mockRestore();
});

it("opens the exact saved session when clicking between existing tasks", async () => {
  const client = new QueryClient();
  const sessionScope = "switch-test";
  const tasks: Item[] = [
    { id: 9, title: "Latest task", kind: "execution", stage: "planned" },
    { id: 8, title: "Earlier task", kind: "execution", stage: "developing" },
  ];
  localStorage.setItem(
    "horizonstudio:task-session:switch-test:9",
    "session-latest",
  );
  localStorage.setItem(
    "horizonstudio:task-session:switch-test:8",
    "session-earlier",
  );

  function Workspace() {
    const [id, setId] = useState(9);
    return (
      <SessionWorkspace
        active
        project={{ id: 2, root: "/workspace/Horizon" }}
        tasks={tasks}
        requirements={[]}
        mode="all"
        onModeChange={() => {}}
        task={tasks.find((task) => task.id === id)}
        runs={[]}
        sessionScope={sessionScope}
        showInspector={false}
        showArchived={false}
        onShowArchived={() => {}}
        agentStatuses={{}}
        onAgentStatus={() => {}}
        canWrite={false}
        pending={false}
        loaded
        error={undefined}
        onCreate={() => {}}
        onSelect={setId}
        refresh={() => {}}
      />
    );
  }

  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <Workspace />
      </QueryClientProvider>
    </I18nextProvider>,
  );

  const frame = (await screen.findByTitle("DSH session")) as HTMLIFrameElement;
  const target = frame.contentWindow!;
  const send = vi.spyOn(target, "postMessage").mockImplementation(() => {});
  const posted = () =>
    send.mock.calls.map(([message]) => message as AgentRequest);
  const message = (data: Record<string, unknown>) =>
    fireEvent(
      window,
      new MessageEvent("message", {
        origin: agentOrigin,
        source: target,
        data: { protocol: PROTOCOL, ...data },
      }),
    );
  const requestId = (sessionId: string) => {
    const request = posted()
      .filter(
        (message) => message.op === "open" && message.sessionId === sessionId,
      )
      .at(-1);
    expect(request).toBeDefined();
    return request!.requestId;
  };

  expect(frame).toHaveClass("invisible");
  fireEvent.load(frame);
  expect(send).toHaveBeenCalledWith(
    { protocol: PROTOCOL, op: "probe" },
    agentOrigin,
  );
  message({ event: "ready" });
  message({
    requestId: requestId("session-latest"),
    ok: true,
    sessionId: "session-latest",
  });
  await waitFor(() => expect(frame).not.toHaveClass("invisible"));

  fireEvent.click(screen.getByRole("button", { name: /Earlier task/ }));
  const earlierRequest = requestId("session-earlier");
  expect(screen.getByTitle("DSH session")).toBe(frame);
  expect(frame).toHaveClass("invisible");
  fireEvent.click(screen.getByRole("button", { name: /Latest task/ }));
  const latestRequest = requestId("session-latest");
  message({
    requestId: earlierRequest,
    ok: true,
    sessionId: "session-earlier",
  });
  expect(frame).toHaveClass("invisible");
  message({ requestId: latestRequest, ok: true, sessionId: "session-latest" });
  await waitFor(() => expect(frame).not.toHaveClass("invisible"));

  fireEvent.click(screen.getByRole("button", { name: /Earlier task/ }));
  expect(screen.getByTitle("DSH session")).toBe(frame);
  message({
    requestId: requestId("session-earlier"),
    ok: true,
    sessionId: "session-earlier",
  });
  await waitFor(() => expect(frame).not.toHaveClass("invisible"));
});
