import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import {
  Check,
  LoaderCircle,
  LogIn,
  Plus,
  RefreshCw,
  UserPlus,
  X,
} from "lucide-react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import {
  getIdentityListSignInIdentitiesQueryKey,
  identityListSignInIdentities,
} from "@/api/generated/baseline";
import { identityMembershipAccept } from "@/lib/capabilities";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useAuth, useSession } from "./auth";
import { sessionLandingPath } from "./session-navigation";
import {
  DataTable,
  EditorDialog,
  ErrorBanner,
  Loading,
  RowActions,
  type AdminColumn,
} from "./shared";
import type { SignInIdentity } from "@/api/generated/models";
import { ApiError } from "@/api/client/request";
import { SecurityInvitation } from "./security-invitation";

export function SecurityMemberships({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const session = useSession();
  const auth = useAuth();
  const navigate = useNavigate();
  const [accepting, setAccepting] = useState(false);
  const [inviting, setInviting] = useState(false);
  const [switching, setSwitching] = useState(false);
  const switchBusy = useRef(false);
  const active = useRef(true);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  async function switchContext(identity: SignInIdentity) {
    if (switchBusy.current) return;
    switchBusy.current = true;
    setSwitching(true);
    setSwitchError(undefined);
    try {
      const next = await auth.selectSignInIdentity(identity.identityBindingId);
      await navigate(sessionLandingPath(next.portal, "/"));
    } catch (cause) {
      if (active.current) setSwitchError(cause);
    } finally {
      switchBusy.current = false;
      if (active.current) setSwitching(false);
    }
  }
  const [switchError, setSwitchError] = useState<unknown>();
  const contexts = useQuery({
    queryKey: [...getIdentityListSignInIdentitiesQueryKey(), ...queryKey],
    queryFn: async ({ signal }) => {
      const response = await identityListSignInIdentities({ signal });
      if (response.status !== 200)
        throw new ApiError("forbidden", response.status);
      return response.data;
    },
    retry: false,
  });
  const current = (identity: SignInIdentity) =>
    (identity.identityBindingId ?? "") === (session.identityBindingId ?? "");
  const rows = (contexts.data ?? []).map((context, index) => ({
    ...context,
    id: index + 1,
  }));
  type Row = (typeof rows)[number];
  const columns: AdminColumn<Row>[] = [
    {
      id: "organization",
      header: t("admin:security.memberships.organization"),
      cell: (row) => row.actingOrganizationName,
    },
    {
      id: "identity",
      header: t("admin:security.memberships.identity"),
      cell: (row) => row.identityType ?? t("admin:security.memberships.base"),
    },
    {
      id: "current",
      header: t("admin:security.memberships.current"),
      cell: (row) =>
        current(row) ? t("admin:security.memberships.current") : null,
    },
    {
      id: "actions",
      header: t("admin:fields.actions"),
      cell: (row) =>
        current(row) ? null : (
          <RowActions
            actions={[
              {
                label: t("admin:security.memberships.switch"),
                icon: <LogIn />,
                onClick: () => void switchContext(row),
              },
            ]}
          />
        ),
    },
  ];
  return (
    <div className="space-y-4">
      <p className="muted-text">{t("admin:security.memberships.help")}</p>
      <div className="standalone-toolbar flex-wrap">
        <Button
          type="button"
          disabled={switching}
          onClick={() => setAccepting(true)}
        >
          <Plus />
          {t("admin:security.memberships.accept")}
        </Button>
        {session.permissions.includes("accounts:create") &&
          !session.identityBindingId && (
            <Button
              type="button"
              variant="outline"
              disabled={switching}
              onClick={() => setInviting(true)}
            >
              <UserPlus />
              {t("admin:security.memberships.invite")}
            </Button>
          )}
        <Button
          type="button"
          variant="outline"
          disabled={contexts.isFetching || switching}
          onClick={() => void contexts.refetch()}
        >
          <RefreshCw />
          {t("admin:actions.refresh")}
        </Button>
      </div>
      {!!switchError && <ErrorBanner error={switchError} />}
      {contexts.isPending ? (
        <Loading />
      ) : contexts.error ? (
        <ErrorBanner
          error={contexts.error}
          onRetry={() => void contexts.refetch()}
        />
      ) : (
        <DataTable rows={rows} columns={columns} serverPage />
      )}
      {accepting && <AcceptInvitation onClose={() => setAccepting(false)} />}
      {inviting && <SecurityInvitation onClose={() => setInviting(false)} />}
    </div>
  );
}

const invitationSchema = z.object({
  code: z
    .string()
    .trim()
    .regex(/^[a-fA-F0-9]{64}$/),
});
function AcceptInvitation({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const [error, setError] = useState<unknown>();
  const request = useRef({ code: "", key: crypto.randomUUID() });
  const busy = useRef(false);
  const active = useRef(true);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  const form = useForm({
    resolver: zodResolver(invitationSchema),
    defaultValues: { code: "" },
  });
  return (
    <EditorDialog
      open
      title={t("admin:security.memberships.accept")}
      description={t("admin:security.memberships.acceptHelp")}
      onOpenChange={(open) => {
        if (!open && !busy.current) onClose();
      }}
    >
      <form
        className="entity-form"
        onSubmit={(event) => {
          void form.handleSubmit(async ({ code }) => {
            if (busy.current) return;
            busy.current = true;
            setError(undefined);
            if (request.current.code !== code)
              request.current = { code, key: crypto.randomUUID() };
            try {
              await identityMembershipAccept({ code }, request.current.key);
              if (!active.current) return;
              form.reset();
              request.current.code = "";
              await client.invalidateQueries({
                queryKey: getIdentityListSignInIdentitiesQueryKey(),
              });
              if (active.current) onClose();
            } catch (cause) {
              if (active.current) setError(cause);
            } finally {
              busy.current = false;
            }
          })(event);
        }}
      >
        {!!error && <ErrorBanner error={error} />}
        <label className="form-field">
          <span id="invitation-code-label">
            {t("admin:security.memberships.code")}
          </span>
          <Input
            type="password"
            autoComplete="off"
            required
            maxLength={64}
            aria-labelledby="invitation-code-label"
            aria-describedby="invitation-code-help"
            aria-invalid={!!form.formState.errors.code}
            {...form.register("code")}
          />
          <small id="invitation-code-help">
            {t(
              form.formState.errors.code
                ? "admin:security.memberships.invalidCode"
                : "admin:security.memberships.codeHelp",
            )}
          </small>
        </label>
        <div className="form-actions">
          <Button
            type="button"
            variant="outline"
            disabled={form.formState.isSubmitting}
            onClick={onClose}
          >
            <X />
            {t("admin:actions.cancel")}
          </Button>
          <Button type="submit" disabled={form.formState.isSubmitting}>
            {form.formState.isSubmitting ? (
              <LoaderCircle className="animate-spin" />
            ) : (
              <Check />
            )}
            {t("admin:security.memberships.accept")}
          </Button>
        </div>
      </form>
    </EditorDialog>
  );
}
