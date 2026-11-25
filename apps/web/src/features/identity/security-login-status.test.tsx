import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";
import { i18n, changeLanguage } from "@/i18n";
import { SecurityLoginStatus } from "./security-login-status";

beforeEach(async () => {
  await changeLanguage("en");
});
afterEach(() => vi.unstubAllGlobals());

function setup(
  options: { frozen?: boolean; delayProof?: boolean; failProof?: boolean } = {},
) {
  const calls: {
    name: string;
    input: Record<string, unknown>;
    key: string;
    signal?: AbortSignal | null;
  }[] = [];
  let finish: ((response: Response) => void) | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const name = decodeURIComponent(url.split("/")[3]);
      const payload = JSON.parse(String(init?.body));
      calls.push({
        name,
        input: payload.input,
        key: payload.idempotencyKey,
        signal: init?.signal,
      });
      if (name === "identity.login.reauthenticate") {
        if (options.delayProof)
          return new Promise<Response>((resolve) => {
            finish = resolve;
          });
        if (options.failProof)
          return Response.json({ status: "failed", errorCode: "forbidden" });
        return Response.json({
          status: "succeeded",
          data: { proof: "p".repeat(64) },
        });
      }
      if (name !== "identity.login.freeze" && name !== "identity.login.restore")
        throw new Error("Unexpected capability");
      return Response.json({
        status: "succeeded",
        data: { id: 105, status: options.frozen ? "active" : "frozen" },
      });
    }),
  );
  const changed = vi.fn();
  const close = vi.fn();
  const view = render(
    <I18nextProvider i18n={i18n}>
      <SecurityLoginStatus
        member={{
          id: 105,

          username: "partner.login",

          membershipStatus: "active",
          accountStatus: "active",
          loginStatus: options.frozen ? "frozen" : "active",
          principalKind: "human",
          protected: false,
          expiresAt: "",
        }}
        onClose={close}
        onChanged={changed}
      />
    </I18nextProvider>,
  );
  return {
    ...view,
    changed,
    close,
    calls,
    finish: () =>
      finish?.(
        Response.json({ status: "succeeded", data: { proof: "p".repeat(64) } }),
      ),
  };
}

async function fill() {
  const user = userEvent.setup();
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "TestOnly!2026Secure",
  );
  await user.type(
    screen.getByRole("textbox", { name: "Change reason" }),
    "Reviewed by security officer",
  );
  return user;
}

it.each([false, true])(
  "reauthenticates before changing sign-in state (frozen=%s)",
  async (frozen) => {
    const view = setup({ frozen });
    const user = await fill();
    await user.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(view.changed).toHaveBeenCalledOnce());
    const operation = frozen ? "restore" : "freeze";
    expect(view.calls.map((call) => call.name)).toEqual([
      "identity.login.reauthenticate",
      "identity.login." + operation,
    ]);
    expect(view.calls[0].input).toEqual({
      accountId: 105,
      operation,
      password: "TestOnly!2026Secure",
    });
    expect(view.calls[1].input).toEqual({
      accountId: 105,
      proof: "p".repeat(64),
      reason: "Reviewed by security officer",
    });
    expect(view.calls[1].key).toBe(
      view.calls[0].key.replace(":proof", ":change"),
    );
    view.unmount();
  },
);

it("requires a reason and never changes state after a failed password proof", async () => {
  const view = setup({ failProof: true });
  const user = userEvent.setup();
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "WrongPassword",
  );
  await user.click(screen.getByRole("button", { name: "Confirm" }));
  expect(view.calls).toHaveLength(0);
  await user.type(
    screen.getByRole("textbox", { name: "Change reason" }),
    "Reviewed by security officer",
  );
  await user.click(screen.getByRole("button", { name: "Confirm" }));
  await waitFor(() =>
    expect(screen.getByLabelText("Password", { exact: true })).toHaveValue(""),
  );
  expect(view.calls).toHaveLength(1);
  expect(view.calls[0].name).toBe("identity.login.reauthenticate");
  expect(view.changed).not.toHaveBeenCalled();
  expect(screen.getByRole("textbox", { name: "Change reason" })).toHaveValue(
    "Reviewed by security officer",
  );
  view.unmount();
});

it("does not continue a proof chain after its context unmounts", async () => {
  const view = setup({ delayProof: true });
  const user = await fill();
  await user.click(screen.getByRole("button", { name: "Confirm" }));
  await waitFor(() => expect(view.calls).toHaveLength(1));
  expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  view.unmount();
  expect(view.calls[0].signal?.aborted).toBe(true);
  await act(async () => {
    view.finish();
  });
  expect(view.calls).toHaveLength(1);
  expect(view.changed).not.toHaveBeenCalled();
  expect(view.close).not.toHaveBeenCalled();
});
