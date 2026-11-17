import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import RunControls from "./run-controls";
import { studio } from "./api";

vi.mock("./api", () => ({
  studio: {
    control: vi.fn(),
    status: vi.fn(),
    log: vi.fn().mockResolvedValue(""),
  },
}));

const project = {
  id: 7,
  root: "/workspace/Horizon",
};

function renderToolbar(target = project) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <header>
          <RunControls project={target} />
        </header>
      </QueryClientProvider>
    </I18nextProvider>,
  );
}

it("switches toolbar actions after confirmed start and stop", async () => {
  const user = userEvent.setup();
  let running = false;
  const services = () => ({
    services: [
      {
        service: "backend",
        status: running ? "running" : "stopped",
        origin: running ? "http://127.0.0.1:8087" : undefined,
      },
    ],
  });
  vi.mocked(studio.status).mockImplementation(async () => services());
  vi.mocked(studio.control).mockImplementation(async (_project, action) => {
    if (action === "start") running = true;
    if (action === "stop") running = false;
    return services();
  });

  renderToolbar();
  const start = await screen.findByRole("button", { name: "Start" });
  expect(start.closest("header")).toBe(screen.getByRole("banner"));
  expect(screen.queryByRole("link", { name: "Visit" })).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Restart" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Stop" }),
  ).not.toBeInTheDocument();

  await user.click(start);
  const stop = await screen.findByRole("button", { name: "Stop" });
  expect(screen.getByRole("button", { name: "Restart" })).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "Start" }),
  ).not.toBeInTheDocument();
  expect(studio.control).toHaveBeenCalledWith(7, "start", "all");
  expect(screen.getByRole("link", { name: "Visit" })).toHaveAttribute(
    "href",
    "http://127.0.0.1:8087/api/health",
  );

  await waitFor(() => expect(stop).toBeEnabled());
  await user.click(stop);
  expect(await screen.findByRole("button", { name: "Start" })).toBeVisible();
  expect(screen.queryByRole("link", { name: "Visit" })).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Restart" }),
  ).not.toBeInTheDocument();
});

it.each([
  { statuses: ["running", "stopped"], label: "1/2 running", canStop: true },
  { statuses: ["unknown", "stopped"], label: "Unknown", canStop: false },
])(
  "shows only actions supported by $statuses",
  async ({ statuses, label, canStop }) => {
    vi.mocked(studio.status).mockResolvedValue({
      services: statuses.map((status, index) => ({
        service: index === 0 ? "backend" : "frontend",
        status,
      })),
    });
    renderToolbar();
    await screen.findByRole("button", { name: label });
    expect(
      screen.queryByRole("button", { name: "Start" }),
    ).not.toBeInTheDocument();
    if (canStop) {
      expect(screen.getByRole("button", { name: "Restart" })).toBeVisible();
      expect(screen.getByRole("button", { name: "Stop" })).toBeVisible();
    } else {
      expect(
        screen.queryByRole("button", { name: "Restart" }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Stop" }),
      ).not.toBeInTheDocument();
    }
  },
);

it("prefers the running frontend and opens it in a separate tab", async () => {
  vi.mocked(studio.status).mockResolvedValue({
    services: [
      {
        service: "backend",
        status: "running",
        origin: "http://127.0.0.1:8087",
      },
      {
        service: "frontend",
        status: "running",
        origin: "http://127.0.0.1:5179",
      },
    ],
  });
  renderToolbar();
  const link = await screen.findByRole("link", { name: "Visit" });
  expect(link).toHaveAttribute("href", "http://127.0.0.1:5179/");
  expect(link).toHaveAttribute("target", "_blank");
  expect(link).toHaveAttribute("rel", "noopener noreferrer");
});

it("leaves out the visit link when the only running service has an invalid origin", async () => {
  vi.mocked(studio.status).mockResolvedValue({
    services: [
      { service: "backend", status: "stopped" },
      {
        service: "frontend",
        status: "running",
        origin: "http://unrelated.example:5173",
      },
    ],
  });
  renderToolbar();
  await screen.findByRole("button", { name: "Restart" });
  expect(screen.queryByRole("link", { name: "Visit" })).not.toBeInTheDocument();
});
