import { fireEvent, render, screen, within } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import type { AgentSessionStatus } from "./agent-connection";
import type { Item, Requirement } from "./api";
import { RequirementGroup, SessionRows } from "./session-rows";

const task = (overrides: Partial<Item> = {}): Item => ({
  id: 11,
  kind: "execution",
  title: "Account flow conversation",
  stage: "planned",
  requirementId: 12,
  ...overrides,
});

const status = (
  sessionId: string,
  overrides: Partial<AgentSessionStatus> = {},
): AgentSessionStatus => ({
  sessionId,
  running: false,
  completed: false,
  error: false,
  ...overrides,
});

function sessions(
  tasks: Item[],
  canWrite = true,
  agentStatuses: Record<string, AgentSessionStatus> = {},
) {
  const onSelect = vi.fn();
  const onPin = vi.fn();
  const onArchive = vi.fn();
  render(
    <I18nextProvider i18n={i18n}>
      <SessionRows
        tasks={tasks}
        selectedId={tasks[0]?.id}
        canWrite={canWrite}
        busy={false}
        agentStatuses={agentStatuses}
        onSelect={onSelect}
        onPin={onPin}
        onArchive={onArchive}
      />
    </I18nextProvider>,
  );
  return { onSelect, onPin, onArchive };
}

it("keeps session rows free of stage text and exposes pin and archive actions", () => {
  const { onPin, onArchive } = sessions([task()]);
  expect(screen.getByText("Account flow conversation")).toBeVisible();
  expect(screen.queryByText("Planned")).not.toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Pin session" }));
  expect(onPin).toHaveBeenCalledWith(11, true);
  fireEvent.click(screen.getByRole("button", { name: "Archive session" }));
  expect(onArchive).toHaveBeenCalledWith(11, true);
});

it("keeps pin and archive hidden until the row is hovered or focused", () => {
  sessions([task()]);
  const pin = screen.getByRole("button", { name: "Pin session" });
  expect(pin.parentElement).toHaveClass("opacity-0");
  expect(pin.parentElement).toHaveClass("group-hover/session:opacity-100");
});

it("labels pinned and archived rows with their reverse action", () => {
  const { onPin, onArchive } = sessions([
    task({ pinned: true, archived: true }),
  ]);
  fireEvent.click(screen.getByRole("button", { name: "Unpin session" }));
  expect(onPin).toHaveBeenCalledWith(11, false);
  fireEvent.click(screen.getByRole("button", { name: "Unarchive session" }));
  expect(onArchive).toHaveBeenCalledWith(11, false);
});

it("hides session actions without write permission", () => {
  sessions([task()], false);
  expect(
    screen.queryByRole("button", { name: "Pin session" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Archive session" }),
  ).not.toBeInTheDocument();
});

it("shows a working spinner for a running DSH session", () => {
  sessions([task({ dshSessionId: "dsh-running" })], true, {
    "dsh-running": status("dsh-running", { running: true }),
  });
  expect(screen.getByRole("status", { name: "Working" })).toBeVisible();
});

it("shows the completion dot until the session is opened", () => {
  sessions([task({ dshSessionId: "dsh-done" })], true, {
    "dsh-done": status("dsh-done", { completed: true }),
  });
  expect(screen.getByRole("img", { name: "Turn completed" })).toBeVisible();
});

it("prefers the failure icon over a running state", () => {
  sessions([task({ dshSessionId: "dsh-failed" })], true, {
    "dsh-failed": status("dsh-failed", { running: true, error: true }),
  });
  expect(screen.getByRole("img", { name: "Turn failed" })).toBeVisible();
  expect(
    screen.queryByRole("status", { name: "Working" }),
  ).not.toBeInTheDocument();
});

function requirement(overrides: Partial<Requirement> = {}): Requirement {
  return { id: 12, title: "Account flow requirement", ...overrides };
}

function group(
  row: Requirement,
  options: {
    tasks?: Item[];
    agentStatuses?: Record<string, AgentSessionStatus>;
  } = {},
) {
  const onCreate = vi.fn();
  render(
    <I18nextProvider i18n={i18n}>
      <RequirementGroup
        requirement={row}
        open
        tasks={options.tasks ?? []}
        canWrite
        busy={false}
        agentStatuses={options.agentStatuses ?? {}}
        onCreate={onCreate}
        onSelect={() => {}}
        onPin={() => {}}
        onArchive={() => {}}
      />
    </I18nextProvider>,
  );
  return { onCreate };
}

it("shows the requirement status directly after its name and creates sessions from the row", () => {
  const { onCreate } = group(requirement());
  const title = screen.getByText("Account flow requirement");
  expect(title.nextElementSibling).toHaveTextContent("Active");
  const create = screen.getByRole("button", { name: "New task" });
  expect(create).toHaveClass("opacity-0");
  expect(create).toHaveClass("group-hover/requirement:opacity-100");
  fireEvent.click(create);
  expect(onCreate).toHaveBeenCalledWith(12);
  expect(
    screen.getByText("No sessions linked to this requirement."),
  ).toBeVisible();
});

it("keeps archived requirements readable without offering new sessions", () => {
  group(requirement({ archived: true }));
  expect(screen.getByText("Archived")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "New task" }),
  ).not.toBeInTheDocument();
});

function summaryOf(title: string) {
  return screen.getByText(title).closest("summary") as HTMLElement;
}

it("mirrors a running child session on the requirement", () => {
  group(requirement(), {
    tasks: [
      task({ id: 21, title: "Running task", dshSessionId: "dsh-run" }),
      task({ id: 22, title: "Done task", dshSessionId: "dsh-done" }),
    ],
    agentStatuses: {
      "dsh-run": status("dsh-run", { running: true }),
      "dsh-done": status("dsh-done", { completed: true }),
    },
  });
  const summary = summaryOf("Account flow requirement");
  expect(
    within(summary).getByRole("status", { name: "Working" }),
  ).toBeVisible();
  expect(
    within(summary).queryByRole("img", { name: "Turn completed" }),
  ).not.toBeInTheDocument();
});

it("shows a failure on the requirement ahead of an unseen completion", () => {
  group(requirement(), {
    tasks: [
      task({ id: 23, title: "Failed task", dshSessionId: "dsh-fail" }),
      task({ id: 24, title: "Done task", dshSessionId: "dsh-done" }),
    ],
    agentStatuses: {
      "dsh-fail": status("dsh-fail", { error: true }),
      "dsh-done": status("dsh-done", { completed: true }),
    },
  });
  const summary = summaryOf("Account flow requirement");
  expect(
    within(summary).getByRole("img", { name: "Turn failed" }),
  ).toBeVisible();
  expect(
    within(summary).queryByRole("img", { name: "Turn completed" }),
  ).not.toBeInTheDocument();
});
