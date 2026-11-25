import { expect, it } from "vitest";
import { defaultLandingPath } from "./router";
import { sessionLandingPath } from "@/features/identity/session-navigation";

it("lands on the AI workbench when the account holds the agent grant", () => {
  expect(defaultLandingPath(["assistant:use", "accounts:read"])).toBe("/ai");
});

it("falls back to statistics instead of a workbench it may not open", () => {
  expect(defaultLandingPath([])).toBe("/analytics");
  expect(defaultLandingPath(["accounts:read"])).toBe("/analytics");
});

it("opens account security for sessions with the retired supplier portal", () => {
  expect(sessionLandingPath("supplier-portal", "/analytics")).toBe(
    "/account-security",
  );
  expect(sessionLandingPath("supplier", "/analytics")).toBe("/supplier");
  expect(sessionLandingPath(undefined, "/analytics")).toBe("/analytics");
});
