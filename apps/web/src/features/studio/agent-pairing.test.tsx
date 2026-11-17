import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { it, expect, vi } from "vitest";
import type { SessionProfile } from "@/api/generated/models";
import { pairStudioMcpAfterLogin } from "./studio-mcp-pairing";
import { ApiError } from "@/api/client/request";
import { i18n } from "@/i18n";
import { mcpManagementCreate } from "@/lib/capabilities";
import { AgentPairing } from "./agent-pairing";
import { agentOrigin, PROTOCOL } from "./agent-connection";

vi.mock("@/lib/capabilities", () => ({ mcpManagementCreate: vi.fn() }));
vi.mock("./agent-connection", async (original) => ({
  ...(await original<typeof import("./agent-connection")>()),
  getAgentConnection: async () => ({
    data: { url: "http://127.0.0.1:3095/?token=test" },
  }),
  useAgentConnection: () => ({
    data: { data: { url: "http://127.0.0.1:3095/?token=test" } },
    isError: false,
  }),
}));

it("reauthenticates the current profile and sends the credential only to DSH", async () => {
  const user = userEvent.setup();
  const create = vi.mocked(mcpManagementCreate);
  create.mockRejectedValueOnce(new ApiError("forbidden", 403));
  create.mockResolvedValueOnce({
    status: 200,
    data: { secret: "fake-one-time-studio-credential" },
  } as Awaited<ReturnType<typeof mcpManagementCreate>>);
  render(
    <I18nextProvider i18n={i18n}>
      <AgentPairing sessionScope="tenant:1:2" accountId={1} departmentId={2} />
    </I18nextProvider>,
  );

  await user.click(screen.getByRole("button", { name: "Connect MCP" }));
  const frame = (await screen.findByTitle("Connect MCP")) as HTMLIFrameElement;
  const target = frame.contentWindow!;
  const send = vi.spyOn(target, "postMessage").mockImplementation(() => {});
  const reply = (data: Record<string, unknown>, origin = agentOrigin) =>
    fireEvent(
      window,
      new MessageEvent("message", {
        origin,
        source: target,
        data: { protocol: PROTOCOL, ...data },
      }),
    );
  const submit = screen.getByRole("button", { name: "Verify and connect" });
  reply({ event: "mcp-pair-ready" }, "http://127.0.0.1:3000");
  expect(submit).toBeDisabled();
  reply({ event: "mcp-pair-ready" });
  const password = screen.getByLabelText("Current Horizon password");
  await user.type(password, "wrong-test-password");
  await user.click(submit);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Account verification failed",
  );
  expect(send).not.toHaveBeenCalled();

  await user.type(password, "correct-test-password");
  await user.click(submit);
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith(
      expect.objectContaining({
        op: "mcp-pair",
        credential: {
          token: "fake-one-time-studio-credential",
          accountId: 1,
          departmentId: 2,
        },
      }),
      agentOrigin,
    ),
  );
  expect(create).toHaveBeenLastCalledWith(
    {
      name: "HorizonStudio DSH",
      days: 30,
      departmentId: 2,
      password: "correct-test-password",
    },
    expect.any(AbortSignal),
  );
  const [request] = send.mock.lastCall!;
  reply(
    { event: "mcp-pair-result", requestId: request.requestId, ok: true },
    "http://127.0.0.1:3000",
  );
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
  reply({ event: "mcp-pair-result", requestId: request.requestId, ok: true });
  expect(await screen.findByRole("status")).toHaveTextContent(
    "Credential saved",
  );
  expect(
    screen.queryByText("fake-one-time-studio-credential"),
  ).not.toBeInTheDocument();
});

it("reuses a valid DSH credential for this account without issuing a new token", async () => {
  vi.mocked(mcpManagementCreate).mockClear();
  const profile = {
    permissions: ["studio:read"],
    account: { id: 12 },
    activeDepartmentId: 3,
  } as SessionProfile;
  const pairing = pairStudioMcpAfterLogin(
    profile,
    "test-login-password",
    () => true,
  );
  const frame = await waitFor(() => {
    const element = document.querySelector("iframe");
    expect(element?.src).toContain("#horizonstudio-mcp");
    return element!;
  });
  const target = frame.contentWindow!;
  const send = vi.spyOn(target, "postMessage").mockImplementation(() => {});
  const reply = (data: Record<string, unknown>) =>
    fireEvent(
      window,
      new MessageEvent("message", {
        origin: agentOrigin,
        source: target,
        data: { protocol: PROTOCOL, ...data },
      }),
    );
  reply({ event: "mcp-pair-ready" });
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith(
      expect.objectContaining({
        op: "mcp-pair-check",
        credential: { accountId: 12, departmentId: 3 },
      }),
      agentOrigin,
    ),
  );
  const [request] = send.mock.lastCall!;
  reply({
    event: "mcp-pair-result",
    requestId: request.requestId,
    ok: true,
    connected: true,
  });
  await pairing;
  expect(mcpManagementCreate).not.toHaveBeenCalled();
  expect(frame.isConnected).toBe(false);
});
