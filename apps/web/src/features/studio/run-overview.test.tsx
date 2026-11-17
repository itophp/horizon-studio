import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import { studio } from "./api";
import { RunOverview } from "./run-overview";

vi.mock("./api", () => ({ studio: { status: vi.fn(), log: vi.fn() } }));

it("shows each service status and loads diagnostics from the selected service log", async () => {
  const onSelect = vi.fn();
  const client = new QueryClient();
  vi.mocked(studio.status).mockResolvedValue({
    services: [
      {
        service: "backend",
        status: "running",
        pid: 47,
        startedAt: "2026-11-15T16:00:00Z",
      },
      { service: "frontend", status: "stopped", reason: "process_exited" },
    ],
  });
  vi.mocked(studio.log).mockImplementation(async (_project, name) =>
    name === "backend" ? "API started" : "Error: Port 5173 is already in use",
  );

  const view = (selected: string) => (
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <RunOverview project={7} selected={selected} onSelect={onSelect} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  const page = render(view("backend"));
  expect(await screen.findByText("API started")).toBeVisible();
  expect(await screen.findByText("Started at")).toBeVisible();
  expect(screen.getByText("47")).toBeVisible();
  fireEvent.click(
    screen.getByRole("button", { name: "Frontend Unexpected exit" }),
  );
  expect(onSelect).toHaveBeenCalledWith("frontend");

  page.rerender(view("frontend"));
  expect(
    await screen.findByText(
      "The last started process exited without a recorded stop.",
    ),
  ).toBeVisible();
  expect(
    await screen.findAllByText("Error: Port 5173 is already in use"),
  ).toHaveLength(2);
  expect(screen.queryByText("API started")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Pause updates" }));
  expect(screen.getByRole("button", { name: "Resume updates" })).toBeVisible();
});
