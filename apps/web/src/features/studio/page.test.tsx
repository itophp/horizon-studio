import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { i18n } from "@/i18n";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { expect, it, vi } from "vitest";
import { StudioPage } from "./page";
import { agentOrigin, PROTOCOL } from "./agent-connection";
import { StudioProjectManagementPage } from "./project-management";
import { studio, type Summary } from "./api";

vi.mock("@/features/identity/auth", () => ({
  useSession: () => ({
    tenantId: "tenant",
    account: { id: 1 },
    permissions: ["studio:read", "studio:write"],
  }),
}));
vi.mock("@/features/application-runtime/catalog", () => ({
  useApplicationCatalog: () => ({
    isPending: false,
    isError: false,
    data: { items: [{ id: "studio", route: "/studio" }] },
  }),
}));
vi.mock("./session", () => ({ default: () => null }));
vi.mock("./api", async (load) => ({
  ...(await load<typeof import("./api")>()),
  studio: {
    addItem: vi.fn(async () => ({ id: 17 })),
    addRequirement: vi.fn(async ({ title }: { title: string }) => ({
      id: 14,
      parentId: 12,
      title,
      archived: false,
    })),
    editRequirement: vi.fn(async () => ({
      id: 12,
      parentId: 0,
      title: "Edited requirement",
      archived: false,
    })),
    searchRequirements: vi.fn(async () => [
      { id: 12, title: "Account flow requirement" },
    ]),
    requirementAttachments: vi.fn(async () => []),
    addRequirementAttachment: vi.fn(async () => ({ fileId: 1 })),
    removeRequirementAttachment: vi.fn(async () => ({ fileId: 1 })),
    projects: vi.fn(async () => [{ id: 7, root: "/workspace/Horizon" }]),
    status: vi.fn(async () => ({ services: [] })),
    environment: vi.fn(async () => ({
      ready: false,
      result: [
        { name: "web_dependencies", status: "unavailable", required: true },
      ],
    })),
    summary: vi.fn(async () => ({
      items: [
        {
          id: 11,
          title: "Account flow conversation",
          kind: "execution",
          stage: "planned",
          requirementId: 12,
        },
      ],
      requirements: [
        {
          id: 12,
          title: "Account flow requirement",
          description: "Let operators register accounts.",
        },
      ],
      runs: [],
    })),
    catalog: vi.fn(async () => [
      { id: "accounts.read", version: "1.0.0", title: "Read accounts" },
    ]),
    catalogEntry: vi.fn(async () => ({
      id: "accounts.read",
      version: "1.0.0",
      definition: { Permission: "accounts:read" },
    })),
    pinSession: vi.fn(
      async (_project: number, id: number, pinned: boolean) => ({
        id,
        pinned,
      }),
    ),
    archiveSession: vi.fn(
      async (_project: number, id: number, archived: boolean) => ({
        id,
        archived,
        pinned: false,
      }),
    ),
  },
}));

function Location() {
  return <output>{useLocation().search}</output>;
}

it("switches from requirements to linked sessions and opens fixed-version capability details", async () => {
  const user = userEvent.setup();
  const query = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={query}>
        <MemoryRouter initialEntries={["/studio?project=7"]}>
          <StudioPage />
          <Location />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );

  await user.click(await screen.findByRole("button", { name: "Requirements" }));
  expect(
    screen.queryByRole("link", { name: "Project management" }),
  ).not.toBeInTheDocument();
  const requirementList = screen
    .getByRole("heading", { name: "Requirements" })
    .closest("aside");
  await user.click(
    within(requirementList as HTMLElement).getByText(
      "Account flow requirement",
    ),
  );
  expect(
    await screen.findByText("Let operators register accounts."),
  ).toBeVisible();
  const linkedSessions = screen
    .getByRole("heading", { name: "Linked sessions" })
    .closest("section");
  await user.click(
    within(linkedSessions as HTMLElement).getByRole("button", {
      name: "Account flow conversation",
    }),
  );
  expect(
    await screen.findByRole("heading", { name: "Account flow conversation" }),
  ).toBeVisible();

  await user.click(screen.getByRole("button", { name: "Capabilities" }));
  await user.click(
    await screen.findByRole("button", { name: /Read accounts/ }),
  );
  expect(
    await screen.findByText("accounts:read", { exact: true }),
  ).toBeVisible();
  expect(studio.catalogEntry).toHaveBeenCalledWith(
    7,
    "capability",
    "accounts.read@1.0.0",
    expect.any(AbortSignal),
  );
  expect(screen.getByRole("status")).toHaveTextContent("project=7");
});

it("opens DSH settings over the current Studio view and only accepts messages from its frame", async () => {
  const user = userEvent.setup();
  const query = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  query.setQueryData(["studio-agent", "tenant:1:"], {
    data: { url: `${agentOrigin}/?token=test` },
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={query}>
        <MemoryRouter initialEntries={["/studio?project=7&task=11&view=run"]}>
          <StudioPage />
          <Location />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );

  const trigger = await screen.findByRole("button", { name: "DSH settings" });
  expect(trigger.parentElement?.lastElementChild).toBe(trigger);
  await user.click(trigger);
  const dialog = screen.getByRole("dialog", { name: "DSH settings" });
  const frame = within(dialog).getByTitle("DSH settings") as HTMLIFrameElement;
  expect(frame).toHaveAttribute(
    "src",
    `${agentOrigin}/?token=test#horizonstudio-settings`,
  );
  expect(frame).toHaveAttribute("aria-hidden", "true");

  const send = (event: string, origin: string, source: Window | null) => {
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          origin,
          source,
          data: { protocol: PROTOCOL, event },
        }),
      );
    });
  };
  send("settings-opened", agentOrigin, null);
  send("settings-opened", "http://127.0.0.1:1295", frame.contentWindow);
  expect(frame).toHaveAttribute("aria-hidden", "true");

  send("settings-opened", agentOrigin, frame.contentWindow);
  expect(frame).toHaveAttribute("aria-hidden", "false");
  send("settings-closed", agentOrigin, frame.contentWindow);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.getByText("?project=7&task=11&view=run")).toBeVisible();
});

it("defaults to requirements and reveals their sessions before switching to the flat list", async () => {
  const user = userEvent.setup();
  const query = new QueryClient();
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={query}>
        <MemoryRouter initialEntries={["/studio?project=7"]}>
          <StudioPage />
          <Location />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );

  const mode = await screen.findByRole("tab", { name: "By requirement" });
  expect(mode).toHaveAttribute("aria-selected", "true");
  const group = await screen.findByText("Account flow requirement");
  expect(group.closest("details")).not.toHaveAttribute("open");
  await user.click(group);
  expect(group.closest("details")).toHaveAttribute("open");
  await user.click(
    screen.getByRole("button", { name: /Account flow conversation/ }),
  );
  expect(
    screen.getByRole("heading", { name: "Account flow conversation" }),
  ).toBeVisible();

  await user.click(screen.getByRole("tab", { name: "All" }));
  expect(
    screen.queryByText("Account flow requirement"),
  ).not.toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: /Account flow conversation/ }),
  ).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByRole("status")).toHaveTextContent("sessionMode=all");
});

it("opens project details in the admin workspace and reserves the new tab for Studio", async () => {
  const user = userEvent.setup();
  const query = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={query}>
        <MemoryRouter initialEntries={["/studio-projects"]}>
          <Routes>
            <Route
              path="/studio-projects/:projectId?"
              element={<StudioProjectManagementPage />}
            />
            <Route path="/studio" element={<StudioPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );

  expect(
    await screen.findByRole("heading", { name: "Project management" }),
  ).toBeVisible();
  const studioLink = await screen.findByRole("link", {
    name: "Studio",
  });
  expect(studioLink).toHaveAttribute("href", "/studio?project=7");
  expect(studioLink).toHaveAttribute("target", "_blank");
  const detailLink = screen.getByRole("link", { name: "Project details" });
  expect(detailLink).toHaveAttribute("href", "/studio-projects/7");
  expect(detailLink).not.toHaveAttribute("target");

  await user.click(detailLink);
  expect(await screen.findByRole("heading", { name: "Horizon" })).toBeVisible();
  expect(screen.getByRole("link", { name: "Studio" })).toHaveAttribute(
    "target",
    "_blank",
  );
  const tabs = screen.getByRole("tablist", { name: "Project details" });
  expect(tabs).toHaveClass("admin-page-tabs");
  const requirements = within(tabs).getByRole("tab", { name: "Requirements" });
  expect(requirements).toHaveAttribute("aria-selected", "true");
  expect(
    screen.queryByRole("img", { name: "HorizonStudio" }),
  ).not.toBeInTheDocument();

  const initialization = within(tabs).getByRole("tab", {
    name: "Initialization",
  });
  await user.click(initialization);
  expect(initialization).toHaveAttribute("aria-selected", "true");
  expect(requirements).toHaveAttribute("aria-selected", "false");
  const panel = screen.getByRole("tabpanel", { name: "Initialization" });
  expect(
    await within(panel).findByText("pnpm install --frozen-lockfile"),
  ).toBeVisible();

  await user.keyboard("{Home}{Enter}");
  expect(requirements).toHaveAttribute("aria-selected", "true");
});

it("prefills a new Studio task from a requirement and sends the link through the gateway", async () => {
  const query = new QueryClient();
  const user = userEvent.setup();
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={query}>
        <MemoryRouter
          initialEntries={["/studio?project=7&view=requirements&entry=12"]}
        >
          <StudioPage />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Create Studio task" }),
  );
  const modal = screen.getByRole("dialog");
  expect(within(modal).getByRole("combobox")).toHaveValue("12");
  await user.click(within(modal).getByRole("button", { name: "Create task" }));
  expect(studio.addItem).toHaveBeenCalledWith(
    expect.objectContaining({
      project: 7,
      kind: "execution",
      title: "Account flow requirement",
      requirementId: 12,
    }),
  );
});

it("edits a requirement through the gateway", async () => {
  const query = new QueryClient();
  const user = userEvent.setup();
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={query}>
        <MemoryRouter
          initialEntries={["/studio?project=7&view=requirements&entry=12"]}
        >
          <StudioPage />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Edit requirement" }),
  );
  const dialog = screen.getByRole("dialog");
  const title = within(dialog).getByLabelText("Requirement title");
  await user.clear(title);
  await user.type(title, "Edited requirement");
  await user.click(
    within(dialog).getByRole("button", { name: "Save changes" }),
  );
  await waitFor(() =>
    expect(studio.editRequirement).toHaveBeenCalledWith(
      expect.objectContaining({
        project: 7,
        id: 12,
        title: "Edited requirement",
        description: "Let operators register accounts.",
      }),
    ),
  );
});

it("renders a requirement tree and creates a sub-requirement", async () => {
  vi.mocked(studio.summary).mockResolvedValueOnce({
    items: [],
    requirements: [
      { id: 12, title: "Account flow requirement", description: "Root body" },
      {
        id: 13,
        parentId: 12,
        title: "Sub requirement",
        description: "Child body",
      },
    ],
    runs: [],
  });
  const query = new QueryClient();
  const user = userEvent.setup();
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={query}>
        <MemoryRouter initialEntries={["/studio?project=7&view=requirements"]}>
          <StudioPage />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );
  const requirementList = (
    await screen.findByRole("heading", { name: "Requirements" })
  ).closest("aside") as HTMLElement;
  expect(
    await within(requirementList).findByText("Account flow requirement"),
  ).toBeVisible();
  expect(within(requirementList).getByText("Sub requirement")).toBeVisible();
  await user.click(
    within(requirementList).getByRole("button", {
      name: "Collapse sub-requirements",
    }),
  );
  expect(
    within(requirementList).queryByText("Sub requirement"),
  ).not.toBeInTheDocument();
  await user.click(
    within(requirementList).getByRole("button", {
      name: "Expand sub-requirements",
    }),
  );
  expect(
    await within(requirementList).findByText("Sub requirement"),
  ).toBeVisible();
  await user.click(
    within(requirementList).getByText("Account flow requirement"),
  );
  await user.click(
    await screen.findByRole("button", { name: "Add sub-requirement" }),
  );
  const dialog = screen.getByRole("dialog");
  await user.type(
    within(dialog).getByLabelText("Requirement title"),
    "New sub",
  );
  await user.click(
    within(dialog).getByRole("button", { name: "Create sub-requirement" }),
  );
  await waitFor(() =>
    expect(studio.addRequirement).toHaveBeenCalledWith(
      expect.objectContaining({
        project: 7,
        parent: 12,
        title: "New sub",
      }),
    ),
  );
});

it("groups sub-requirement sessions under the top-level requirement", async () => {
  vi.mocked(studio.summary).mockResolvedValueOnce({
    items: [
      {
        id: 21,
        kind: "execution",
        title: "Child session",
        stage: "planned",
        requirementId: 13,
      },
    ],
    requirements: [
      { id: 12, parentId: 0, title: "Root requirement" },
      { id: 13, parentId: 12, title: "Child requirement" },
    ],
    runs: [],
  });
  const query = new QueryClient();
  const user = userEvent.setup();
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={query}>
        <MemoryRouter initialEntries={["/studio?project=7"]}>
          <StudioPage />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );
  const root = await screen.findByText("Root requirement");
  expect(screen.queryByText("Child requirement")).not.toBeInTheDocument();
  await user.click(root);
  expect(await screen.findByText("Child session")).toBeVisible();
});

it("only completes a planned workflow when its exact version appears in the CLI", async () => {
  const query = new QueryClient();
  const plan: Summary = {
    items: [
      {
        id: 20,
        kind: "workflow",
        title: "Approval workflow",
        stage: "planned",
        catalogTarget: "approval-flow@1.0.0",
      },
    ],
    requirements: [],
    runs: [],
  };
  vi.mocked(studio.summary).mockResolvedValueOnce(plan);
  vi.mocked(studio.catalog).mockResolvedValueOnce([
    { id: "approval-flow", version: "2.0.0" },
  ]);
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={query}>
        <MemoryRouter
          initialEntries={[
            "/studio?project=7&view=workflows&entry=approval-flow%401.0.0",
          ]}
        >
          <StudioPage />
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );
  expect(
    await screen.findByRole("heading", { name: "Approval workflow" }),
  ).toBeVisible();
  expect(screen.getAllByText("Planned").length).toBeGreaterThan(0);
  act(() =>
    query.setQueryData(["studio", "tenant", 1, undefined, "summary", 7], {
      ...plan,
      items: [
        ...plan.items,
        {
          id: 21,
          kind: "execution",
          title: "Implement approval flow",
          stage: "planned",
          subjectItemId: 20,
        },
      ],
    }),
  );
  expect((await screen.findAllByText("In development")).length).toBeGreaterThan(
    0,
  );
  act(() =>
    query.setQueryData(
      ["studio-catalog", "tenant", 1, undefined, undefined, 7, "workflow"],
      [{ id: "approval-flow", version: "1.0.0" }],
    ),
  );
  expect((await screen.findAllByText("Completed")).length).toBeGreaterThan(0);
  expect(screen.queryByText("In development")).not.toBeInTheDocument();
});
