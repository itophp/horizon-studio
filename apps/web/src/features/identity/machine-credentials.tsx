import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, KeyRound, RotateCcw, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { IdentityMachineCredentialListOutput } from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  machineCredentialList,
  machineCredentialRevoke,
  machineCredentialRotate,
} from "@/lib/capabilities";
import { ApiError } from "@/api/client/request";
import { SecurityConfirm } from "./security-confirm";
import {
  DataTable,
  EditorDialog,
  ErrorBanner,
  Loading,
  ShortDate,
  type AdminColumn,
} from "./shared";

type Credential = IdentityMachineCredentialListOutput["items"][number];

export function MachineCredentials({
  accountId,
  kind,
  name,
  queryKey,
  onClose,
}: {
  accountId: number;
  kind: string;
  name: string;
  queryKey: readonly unknown[];
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [after, setAfter] = useState("");
  const [operation, setOperation] = useState<"rotate" | "revoke">();
  const [rotation, setRotation] = useState<{
    capabilities: string[];
    minutes: number;
  }>();
  const [secret, setSecret] = useState("");
  const form = useRef<HTMLFormElement>(null);
  const credentials = useQuery({
    queryKey: [...queryKey, "machine-credentials", accountId, after],
    queryFn: ({ signal }) =>
      machineCredentialList({ accountId, ...(after ? { after } : {}) }, signal),
    retry: false,
  });
  const columns: AdminColumn<Credential>[] = [
    {
      id: "audience",
      header: t("admin:machine.audience"),
      cell: (entry) => entry.audience,
    },
    {
      id: "capabilities",
      header: t("admin:machine.capabilities"),
      cell: (entry) => entry.capabilities.join(", "),
    },
    {
      id: "expires",
      header: t("admin:security.expires"),
      cell: (entry) => <ShortDate value={entry.expiresAt} />,
    },
    {
      id: "revoked",
      header: t("admin:machine.revokedAt"),
      cell: (entry) =>
        entry.revokedAt ? <ShortDate value={entry.revokedAt} /> : null,
    },
  ];
  const title = t("admin:machine.credentialsFor", { name });
  if (operation) {
    return (
      <SecurityConfirm
        title={t(
          operation === "rotate"
            ? "admin:machine.rotate"
            : "admin:machine.revoke",
        )}
        description={t("admin:machine.invalidate")}
        onClose={() => {
          setOperation(undefined);
          setRotation(undefined);
        }}
        onConfirm={async (password, key, signal) => {
          if (operation === "rotate" && rotation) {
            const response = await machineCredentialRotate(
              {
                accountId,
                audience:
                  kind === "agent" ? "horizon-agent" : "horizon-capability",
                ...rotation,
                password,
              },
              key,
              signal,
            );
            if (signal.aborted) return;
            if (!response.data?.secret) throw new ApiError("conflict", 409);
            setSecret(response.data.secret);
          } else if (operation === "revoke") {
            await machineCredentialRevoke({ accountId, password }, key, signal);
          }
          if (!signal.aborted) void credentials.refetch();
        }}
      />
    );
  }
  return (
    <EditorDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={title}
      description={t("admin:machine.manageHelp")}
    >
      {secret ? (
        <div className="space-y-4">
          <label className="form-field">
            <span>{t("admin:machine.secretOnce")}</span>
            <Input
              value={secret}
              readOnly
              onFocus={(event) => event.currentTarget.select()}
            />
          </label>
          <Button type="button" variant="outline" onClick={() => setSecret("")}>
            <X />
            {t("admin:machine.hideSecret")}
          </Button>
        </div>
      ) : (
        <div className="space-y-4">
          {credentials.isPending ? (
            <Loading />
          ) : credentials.error ? (
            <ErrorBanner
              error={credentials.error}
              onRetry={() => void credentials.refetch()}
            />
          ) : (
            <DataTable
              rows={credentials.data.data.items}
              columns={columns}
              serverPage
            />
          )}
          {(after || credentials.data?.data.nextAfter) && (
            <div className="form-actions">
              <Button
                type="button"
                variant="outline"
                disabled={!after}
                onClick={() => setAfter("")}
              >
                <ArrowUp />
                {t("admin:security.workspaceMembers.first")}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={!credentials.data?.data.nextAfter}
                onClick={() => setAfter(credentials.data?.data.nextAfter ?? "")}
              >
                <ArrowDown />
                {t("admin:actions.next")}
              </Button>
            </div>
          )}
          <form
            className="entity-form"
            ref={form}
            onSubmit={(event) => {
              event.preventDefault();
              const values = new FormData(event.currentTarget);
              const capabilities = String(values.get("capabilities") ?? "")
                .trim()
                .split(/\s+/);
              if (
                capabilities.length > 50 ||
                capabilities.some(
                  (key) =>
                    !/^[a-zA-Z][a-zA-Z0-9._-]{0,79}@\d+\.\d+\.\d+$/.test(key),
                )
              ) {
                const input = form.current?.querySelector("textarea");
                input?.setCustomValidity(
                  t("admin:machine.invalidCapabilities"),
                );
                input?.reportValidity();
                return;
              }
              setRotation({
                capabilities: [...new Set(capabilities)],
                minutes: Number(values.get("minutes")),
              });
              setOperation("rotate");
            }}
          >
            <label className="form-field">
              <span>{t("admin:machine.capabilities")}</span>
              <Textarea
                name="capabilities"
                required
                rows={2}
                maxLength={5000}
                onChange={(event) => event.currentTarget.setCustomValidity("")}
              />
            </label>
            <label className="form-field">
              <span>{t("admin:machine.minutes")}</span>
              <Input
                name="minutes"
                type="number"
                required
                min={1}
                max={1440}
                defaultValue={60}
              />
            </label>
            <div className="form-actions">
              <Button
                type="button"
                variant="outline"
                onClick={() => setOperation("revoke")}
              >
                <KeyRound />
                {t("admin:machine.revoke")}
              </Button>
              <Button type="submit">
                <RotateCcw />
                {t("admin:machine.rotate")}
              </Button>
            </div>
          </form>
        </div>
      )}
    </EditorDialog>
  );
}
