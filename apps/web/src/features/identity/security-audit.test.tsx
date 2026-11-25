import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { i18n, changeLanguage } from "@/i18n";
import { downloadContent } from "@/features/governance/shared";
import { SecurityAudit } from "./security-audit";
import { SecurityPanel } from "./security-panel";

const actor = vi.hoisted(() => ({ permissions: [] as string[] }));
vi.mock("./auth", () => ({
  useSession: () => ({
    tenantId: "tenant-a",
    account: { id: 7 },

    permissions: actor.permissions,
    authorizationVersion: "test",
  }),
  usePermission: (permission: string) => actor.permissions.includes(permission),
}));
vi.mock("@/features/governance/runtime", () => ({
  useConfiguration: () => ({
    value: (_key: string, fallback: unknown) => fallback,
  }),
}));
vi.mock("@/features/governance/shared", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/features/governance/shared")>()),
  downloadContent: vi.fn(),
}));
afterEach(() => vi.unstubAllGlobals());
beforeEach(() => {
  actor.permissions = [];
  vi.clearAllMocks();
});

function entries(first = 100, count = 30) {
  return Array.from({ length: count }, (_, index) => ({
    id: first - index,
    action: "example.record.read",
    outcome: "allowed",
    reason: "rbac",
    accountId: 7,
    resourceType: "example_record",
    resourceId: "123",
    correlationId: `request-${first - index}`,
    at: "2026-11-01T00:00:00Z",
  }));
}

async function setup(
  options: {
    path?: string;
    delayedExport?: boolean;
    approvalRequired?: boolean;
  } = {},
) {
  await changeLanguage("en");
  const calls: {
    name: string;
    input: Record<string, unknown>;
    signal?: AbortSignal | null;
  }[] = [];
  let resolveExport: ((response: Response) => void) | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const name = decodeURIComponent(url.split("/")[3]);
      const input = JSON.parse(String(init?.body)).input;
      calls.push({ name, input, signal: init?.signal });
      if (name === "identity.audit.exportPolicy")
        return Response.json({
          status: "succeeded",
          data: { required: options.approvalRequired ?? false },
        });
      if (name === "identity.audit.exportPermits")
        return Response.json({ status: "succeeded", data: { items: [] } });
      if (name === "identity.audit.requestExport")
        return Response.json({ status: "awaiting_approval" });
      if (name === "identity.audit.export" && options.delayedExport)
        return new Promise<Response>((resolve) => {
          resolveExport = resolve;
        });
      return Response.json({
        status: "succeeded",
        data: {
          items: entries(input.beforeId ? Number(input.beforeId) - 1 : 100),
        },
      });
    }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const wrap = (context: string) => (
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <MemoryRouter
          initialEntries={[options.path ?? "/account-security?tab=audit"]}
        >
          <SecurityAudit queryKey={["audit-test", context]} />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>
  );
  const view = render(wrap("context-a"));
  await screen.findByText("request-100");
  return {
    client,
    calls,
    user: userEvent.setup(),
    rerenderContext: () => view.rerender(wrap("context-b")),
    resolveExport: () =>
      resolveExport?.(
        Response.json({ status: "succeeded", data: { items: entries() } }),
      ),
  };
}

it("uses server cursors and reauthorizes export with the displayed page boundary", async () => {
  const { calls, client, user } = await setup();
  expect(calls[0]).toMatchObject({
    name: "identity.audit.list",
    input: { limit: 30, workspace: false },
  });
  await user.click(screen.getByRole("button", { name: "Older events" }));
  await screen.findByText("request-70");
  expect(calls.at(-1)?.input).toEqual({
    beforeId: 71,
    limit: 30,
    workspace: false,
  });
  await user.click(screen.getByRole("button", { name: "Export current page" }));
  await waitFor(() => expect(downloadContent).toHaveBeenCalledOnce());
  expect(calls.at(-1)).toMatchObject({
    name: "identity.audit.export",
    input: { beforeId: 71, limit: 30, workspace: false },
  });
  const exported = JSON.parse(
    String(vi.mocked(downloadContent).mock.calls[0][1]),
  );
  expect(exported.items[0].id).toBe(70);
  await user.click(screen.getByRole("button", { name: "Latest events" }));
  await screen.findByText("request-100");
  client.clear();
});

it("keeps workspace query parameters from granting access to a regular reader", async () => {
  const { calls, client } = await setup({
    path: "/account-security?tab=audit&auditScope=workspace",
  });
  expect(
    screen.queryByRole("combobox", { name: "Audit scope" }),
  ).not.toBeInTheDocument();
  expect(calls[0].input.workspace).toBe(false);
  client.clear();
});

it("queries the selected workspace scope through Gateway", async () => {
  actor.permissions = ["access:manage"];
  const { calls, client, user } = await setup();
  await user.click(screen.getByRole("combobox", { name: "Audit scope" }));
  await user.click(
    await screen.findByRole("option", { name: "Current tenant" }),
  );
  await waitFor(() => expect(calls.at(-1)?.input.workspace).toBe(true));
  client.clear();
});

it("requests approval for the displayed filtered page before downloading", async () => {
  const { calls, client, user } = await setup({ approvalRequired: true });
  await user.click(screen.getByRole("button", { name: "Filter" }));
  const filters = within(await screen.findByRole("dialog"));
  await user.type(
    filters.getByRole("textbox", { name: "Action" }),
    "example.record.read",
  );
  await user.click(filters.getByRole("button", { name: "Filter" }));
  await user.click(
    await screen.findByRole("button", { name: "Request export" }),
  );
  const request = within(await screen.findByRole("dialog"));
  await user.type(request.getByRole("textbox"), "Review related records");
  await user.click(request.getByRole("button", { name: "Confirm" }));

  await request.findByText(/Request submitted/);
  expect(calls).toContainEqual(
    expect.objectContaining({
      name: "identity.audit.requestExport",
      input: {
        filter: {
          action: "example.record.read",
          beforeId: 101,
          afterId: 70,
          limit: 30,
          workspace: false,
        },
        reason: "Review related records",
        requestId: expect.any(String),
      },
    }),
  );
  expect(calls.some((call) => call.name === "identity.audit.export")).toBe(
    false,
  );
  expect(downloadContent).not.toHaveBeenCalled();
  client.clear();
});

it("cancels an export when its original context is replaced", async () => {
  const { calls, client, user, rerenderContext, resolveExport } = await setup({
    delayedExport: true,
  });
  await user.click(screen.getByRole("button", { name: "Export current page" }));
  await waitFor(() =>
    expect(calls.some((call) => call.name === "identity.audit.export")).toBe(
      true,
    ),
  );
  const original = calls.find((call) => call.name === "identity.audit.export")!;
  rerenderContext();
  await waitFor(() => expect(original.signal?.aborted).toBe(true));
  resolveExport();
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Export current page" }),
    ).toBeEnabled(),
  );
  expect(downloadContent).not.toHaveBeenCalled();
  client.clear();
});

it("does not query audit data from the Runtime preview", async () => {
  await changeLanguage("en");
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  render(
    <I18nextProvider i18n={i18n}>
      <SecurityPanel props={{}} readOnly emit={vi.fn()} />
    </I18nextProvider>,
  );
  expect(fetch).not.toHaveBeenCalled();
});
