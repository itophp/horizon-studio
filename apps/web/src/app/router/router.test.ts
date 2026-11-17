import { expect, it } from "vitest";
import { isValidElement } from "react";
import { matchRoutes } from "react-router";
import {
  AdminLayout,
  AuthBoundary,
  RequireSession,
} from "@/features/administration/layout";
import { defaultWorkspacePath, router } from "./router";

it("lands on the AI workbench when the account holds the agent grant", () => {
  expect(defaultWorkspacePath(["assistant:use", "accounts:read"])).toBe("/ai");
});

it("falls back to statistics instead of a workbench it may not open", () => {
  expect(defaultWorkspacePath([])).toBe("/analytics");
  expect(defaultWorkspacePath(["accounts:read"])).toBe("/analytics");
});

it("matches Studio under shared authentication without the admin layout", () => {
  const matches = matchRoutes(router.routes, "/studio?project=1");
  const elements = matches?.map(({ route }) =>
    isValidElement(route.element) ? route.element.type : null,
  );

  expect(matches?.at(-1)?.route.path).toBe("/studio");
  expect(elements).toContain(AuthBoundary);
  expect(elements).toContain(RequireSession);
  expect(elements).not.toContain(AdminLayout);
});

it.each(["/studio-projects", "/studio-projects/1"])(
  "matches %s in the admin layout",
  (path) => {
    const matches = matchRoutes(router.routes, path);
    const elements = matches?.map(({ route }) =>
      isValidElement(route.element) ? route.element.type : null,
    );

    expect(matches?.at(-1)?.route.path).toBe("/studio-projects/:projectId?");
    expect(elements).toContain(RequireSession);
    expect(elements).toContain(AdminLayout);
  },
);
