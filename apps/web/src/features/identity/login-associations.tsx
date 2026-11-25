import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowUp,
  Check,
  Link2,
  LoaderCircle,
  Unlink,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { IdentityLoginAssociationListOutput } from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  loginAssociationLink,
  loginAssociationList,
  loginAssociationUnlink,
} from "@/lib/capabilities";
import { useSession } from "./auth";
import { SecurityConfirm } from "./security-confirm";
import { LoginMergeReviews } from "./login-merge-reviews";
import {
  DataTable,
  EditorDialog,
  ErrorBanner,
  Loading,
  RowActions,
  ShortDate,
  type AdminColumn,
} from "./shared";

type Association = IdentityLoginAssociationListOutput["items"][number];

export function LoginAssociations({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const session = useSession();
  const [after, setAfter] = useState("");
  const [linkOpen, setLinkOpen] = useState(false);
  const [selected, setSelected] = useState<Association>();
  const [showReviews, setShowReviews] = useState(false);
  const logins = useQuery({
    queryKey: [...queryKey, "login-associations", after],
    queryFn: ({ signal }) =>
      loginAssociationList(after ? { after } : {}, signal),
    retry: false,
  });
  const columns: AdminColumn<Association>[] = [
    {
      id: "username",
      header: t("admin:login.username"),
      cell: (row) => row.username,
    },
    { id: "realm", header: t("admin:login.realm"), cell: (row) => row.realm },
    {
      id: "associated",
      header: t("admin:security.linkedAt"),
      cell: (row) => <ShortDate value={row.createdAt} />,
    },
    {
      id: "actions",
      header: t("admin:fields.actions"),
      cell: (row) => {
        if (row.revokedAt) return <ShortDate value={row.revokedAt} />;
        if (row.placed) return t("admin:loginAssociation.workspaceAssigned");
        return (
          <RowActions
            actions={[
              {
                icon: <Unlink />,
                label: t("admin:loginAssociation.unlink"),
                danger: true,
                onClick: () => setSelected(row),
              },
            ]}
          />
        );
      },
    },
  ];
  const refresh = () => void logins.refetch();
  if (showReviews)
    return (
      <LoginMergeReviews
        queryKey={queryKey}
        onBack={() => {
          setShowReviews(false);
          refresh();
        }}
      />
    );
  return (
    <div className="space-y-4">
      <Button
        type="button"
        variant="outline"
        onClick={() => setShowReviews(true)}
      >
        <Link2 />
        {t("admin:loginMerge.title")}
      </Button>
      {!session.account.isAdmin && !session.platformOperator && (
        <Button type="button" onClick={() => setLinkOpen(true)}>
          <Link2 />
          {t("admin:loginAssociation.link")}
        </Button>
      )}
      {logins.isPending ? (
        <Loading />
      ) : logins.error ? (
        <ErrorBanner error={logins.error} onRetry={refresh} />
      ) : (
        <DataTable rows={logins.data.data.items} columns={columns} serverPage />
      )}
      {(after || logins.data?.data.nextAfter) && (
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
            disabled={!logins.data?.data.nextAfter}
            onClick={() => setAfter(logins.data?.data.nextAfter ?? "")}
          >
            <ArrowDown />
            {t("admin:actions.next")}
          </Button>
        </div>
      )}
      {linkOpen && (
        <LinkLogin
          onClose={() => setLinkOpen(false)}
          onLinked={() => {
            setLinkOpen(false);
            refresh();
          }}
        />
      )}
      {selected && (
        <SecurityConfirm
          title={t("admin:loginAssociation.unlink")}
          description={t("admin:loginAssociation.unlinkHelp", {
            username: selected.username,
          })}
          showFactor
          onClose={() => setSelected(undefined)}
          onConfirm={async (password, key, signal, factor) => {
            await loginAssociationUnlink(
              { id: selected.id, password, ...(factor ? { factor } : {}) },
              key,
              signal,
            );
            if (!signal.aborted) refresh();
          }}
        />
      )}
    </div>
  );
}

function LinkLogin({
  onClose,
  onLinked,
}: {
  onClose: () => void;
  onLinked: () => void;
}) {
  const { t } = useTranslation();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>();
  const request = useRef<AbortController | null>(null);
  const key = useRef(crypto.randomUUID());
  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    return () => controller.abort();
  }, []);
  return (
    <EditorDialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
      title={t("admin:loginAssociation.link")}
      description={t("admin:loginAssociation.linkHelp")}
    >
      <form
        className="entity-form"
        onSubmit={async (event) => {
          event.preventDefault();
          const signal = request.current?.signal;
          if (!signal || signal.aborted || pending) return;
          const fields = new FormData(event.currentTarget);
          const optional = (name: string) =>
            String(fields.get(name) ?? "").trim() || undefined;
          const form = event.currentTarget;
          setPending(true);
          setError(undefined);
          try {
            await loginAssociationLink(
              {
                username: String(fields.get("username")).trim(),
                realm: optional("realm"),
                password: String(fields.get("password")),
                secondaryPassword: String(fields.get("secondaryPassword")),
                factor: optional("factor"),
                secondaryFactor: optional("secondaryFactor"),
              },
              key.current,
              signal,
            );
            if (!signal.aborted) onLinked();
          } catch (cause) {
            if (!signal.aborted) {
              setError(cause);
              form
                .querySelectorAll<HTMLInputElement>('input[type="password"]')
                .forEach((input) => {
                  input.value = "";
                });
              key.current = crypto.randomUUID();
            }
          } finally {
            if (!signal.aborted) setPending(false);
          }
        }}
      >
        {!!error && <ErrorBanner error={error} />}
        <label className="form-field">
          <span>{t("admin:login.username")}</span>
          <Input name="username" minLength={3} maxLength={80} required />
        </label>
        <label className="form-field">
          <span>{t("admin:login.realm")}</span>
          <Input name="realm" maxLength={63} defaultValue="local" required />
        </label>
        <label className="form-field">
          <span>{t("admin:loginAssociation.secondaryPassword")}</span>
          <Input
            name="secondaryPassword"
            type="password"
            autoComplete="off"
            required
            maxLength={128}
          />
        </label>
        <label className="form-field">
          <span>{t("admin:loginAssociation.secondaryFactor")}</span>
          <Input
            name="secondaryFactor"
            type="password"
            pattern="[0-9]{6}|[a-f0-9]{32}"
            maxLength={32}
            autoComplete="off"
          />
        </label>
        <label className="form-field">
          <span>{t("admin:loginAssociation.currentPassword")}</span>
          <Input
            name="password"
            type="password"
            autoComplete="current-password"
            required
            maxLength={128}
          />
        </label>
        <label className="form-field">
          <span>{t("admin:login.secondFactorCode")}</span>
          <Input
            name="factor"
            type="password"
            pattern="[0-9]{6}|[a-f0-9]{32}"
            maxLength={32}
            autoComplete="one-time-code"
          />
        </label>
        <div className="form-actions">
          <Button
            type="button"
            variant="outline"
            onClick={onClose}
            disabled={pending}
          >
            <X />
            {t("admin:actions.cancel")}
          </Button>
          <Button type="submit" disabled={pending}>
            {pending ? <LoaderCircle className="animate-spin" /> : <Check />}
            {t("admin:security.confirm")}
          </Button>
        </div>
      </form>
    </EditorDialog>
  );
}
