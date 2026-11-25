import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowDown,
  CalendarClock,
  LockKeyhole,
  LockKeyholeOpen,
  KeyRound,
  Pause,
  Play,
  RefreshCw,
  UserMinus,
  Settings,
} from "lucide-react";
import type { IdentityMembershipListOutput } from "@/api/generated/capability-identity";
import { identityMembershipList } from "@/lib/capabilities";
import { Button } from "@/components/ui/button";
import { useSession } from "./auth";
import {
  DataTable,
  ErrorBanner,
  Loading,
  RowActions,
  ShortDate,
  type AdminColumn,
} from "./shared";

import { RevokeMembership } from "./security-membership-revoke";
import { SecurityLoginStatus } from "./security-login-status";
import { WorkspaceRegistrationPolicy } from "./workspace-registration-policy";
import { SecurityMembershipChange } from "./security-membership-change";
import { MachineCredentials } from "./machine-credentials";

type Member = IdentityMembershipListOutput["items"][number];

export function SecurityWorkspaceMembers({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const session = useSession();
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  if (
    !session.permissions.includes("access:manage") ||
    session.identityBindingId
  )
    return <p>{t("admin:errors.forbidden")}</p>;
  const raw = params.get("memberAfter") ?? "";
  const after =
    /^[1-9][0-9]*$/.test(raw) && Number.isSafeInteger(Number(raw))
      ? Number(raw)
      : 0;
  const navigate = (cursor: number) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      if (cursor) next.set("memberAfter", String(cursor));
      else next.delete("memberAfter");
      return next;
    });
  return (
    <MemberDirectory
      key={JSON.stringify([...queryKey, after])}
      queryKey={queryKey}
      after={after}
      navigate={navigate}
    />
  );
}

function MemberDirectory({
  queryKey,
  after,
  navigate,
}: {
  queryKey: readonly unknown[];
  after: number;
  navigate: (after: number) => void;
}) {
  const { t } = useTranslation();
  const [registrationOpen, setRegistrationOpen] = useState(false);
  const [selected, setSelected] = useState<Member>();
  const [loginSelected, setLoginSelected] = useState<Member>();
  const [machineSelected, setMachineSelected] = useState<Member>();
  const [membershipChange, setMembershipChange] = useState<{
    member: Member;
    operation: "suspend" | "restore" | "setExpiry";
  }>();
  const session = useSession();
  const members = useQuery({
    queryKey: [...queryKey, "workspace-members", after],
    queryFn: ({ signal }) => identityMembershipList({ after }, signal),
    retry: false,
  });
  const states: Record<string, string> = {
    active: t("admin:security.workspaceMembers.active"),
    pending: t("admin:security.workspaceMembers.pending"),
    suspended: t("admin:security.workspaceMembers.suspended"),
    revoked: t("admin:security.workspaceMembers.revoked"),
    inactive: t("admin:security.workspaceMembers.inactive"),
    frozen: t("admin:security.workspaceMembers.frozen"),
  };
  const state = (value: string) =>
    states[value] ?? t("admin:security.workspaceMembers.unknown");
  const columns: AdminColumn<Member>[] = [
    {
      id: "name",
      header: t("admin:security.workspaceMembers.member"),
      cell: (row) => row.username,
    },
    {
      id: "membership",
      header: t("admin:security.workspaceMembers.membershipState"),
      cell: (row) => state(row.membershipStatus),
    },
    {
      id: "account",
      header: t("admin:security.workspaceMembers.accountState"),
      cell: (row) => state(row.accountStatus),
    },
    {
      id: "login",
      header: t("admin:security.workspaceMembers.loginState"),
      cell: (row) => state(row.loginStatus),
    },
    {
      id: "expires",
      header: t("admin:security.expires"),
      cell: (row) =>
        row.expiresAt ? (
          <ShortDate value={row.expiresAt} />
        ) : (
          t("admin:security.workspaceMembers.noExpiry")
        ),
    },
    {
      id: "actions",
      header: t("admin:fields.actions"),
      cell: (row) => {
        if (row.protected)
          return t("admin:security.workspaceMembers.protected");
        const actions = [];
        if (["agent", "service", "application"].includes(row.principalKind)) {
          actions.push({
            label: t("admin:machine.credentials"),
            icon: <KeyRound />,
            onClick: () => setMachineSelected(row),
          });
        }
        if (row.membershipStatus !== "revoked")
          actions.push({
            label: t("admin:security.workspaceMembers.revoke"),
            icon: <UserMinus />,
            danger: true,
            onClick: () => setSelected(row),
          });
        if (
          row.id !== session.account.id &&
          row.membershipStatus === "active"
        ) {
          actions.push({
            label: t("admin:security.workspaceMembers.suspend"),
            icon: <Pause />,
            danger: true,
            onClick: () =>
              setMembershipChange({ member: row, operation: "suspend" }),
          });
          if (!row.expiresAt || Date.parse(row.expiresAt) > Date.now()) {
            actions.push({
              label: t("admin:security.workspaceMembers.setExpiry"),
              icon: <CalendarClock />,
              danger: false,
              onClick: () =>
                setMembershipChange({ member: row, operation: "setExpiry" }),
            });
          }
        }
        if (
          row.id !== session.account.id &&
          row.membershipStatus === "suspended"
        )
          actions.push({
            label: t("admin:security.workspaceMembers.restoreMembership"),
            icon: <Play />,
            danger: false,
            onClick: () =>
              setMembershipChange({ member: row, operation: "restore" }),
          });
        if (
          session.permissions.includes("accounts:security") &&
          session.account.id !== row.id &&
          ["active", "frozen"].includes(row.loginStatus)
        )
          actions.push({
            label: t(
              row.loginStatus === "frozen"
                ? "admin:security.workspaceMembers.restore"
                : "admin:security.workspaceMembers.freeze",
            ),
            icon:
              row.loginStatus === "frozen" ? (
                <LockKeyholeOpen />
              ) : (
                <LockKeyhole />
              ),
            onClick: () => setLoginSelected(row),
          });
        return actions.length ? <RowActions actions={actions} /> : null;
      },
    },
  ];
  return (
    <div className="space-y-4">
      <p className="muted-text">{t("admin:security.workspaceMembers.help")}</p>
      <div className="standalone-toolbar">
        <Button
          type="button"
          variant="outline"
          onClick={() => setRegistrationOpen(true)}
        >
          <Settings />
          {t("admin:registration.settings")}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={members.isFetching}
          onClick={() => void members.refetch()}
        >
          <RefreshCw />
          {t("admin:actions.refresh")}
        </Button>
      </div>
      {members.isPending ? (
        <Loading />
      ) : members.error ? (
        <ErrorBanner
          error={members.error}
          onRetry={() => void members.refetch()}
        />
      ) : (
        <DataTable
          rows={members.data.data.items}
          columns={columns}
          serverPage
        />
      )}
      <div className="form-actions">
        <Button
          type="button"
          variant="outline"
          disabled={!after || members.isFetching}
          onClick={() => navigate(0)}
        >
          <RefreshCw />
          {t("admin:security.workspaceMembers.first")}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={!members.data?.data.nextAfter || members.isFetching}
          onClick={() => navigate(members.data?.data.nextAfter ?? 0)}
        >
          <ArrowDown />
          {t("admin:actions.next")}
        </Button>
      </div>
      {registrationOpen && (
        <WorkspaceRegistrationPolicy
          queryKey={queryKey}
          onClose={() => setRegistrationOpen(false)}
        />
      )}
      {machineSelected && (
        <MachineCredentials
          accountId={machineSelected.id}
          kind={machineSelected.principalKind}
          name={machineSelected.username}
          queryKey={queryKey}
          onClose={() => setMachineSelected(undefined)}
        />
      )}
      {selected && (
        <RevokeMembership
          key={selected.id}
          member={selected}
          onClose={() => setSelected(undefined)}
          onRevoked={() => {
            setSelected(undefined);
            void members.refetch();
          }}
        />
      )}
      {loginSelected && (
        <SecurityLoginStatus
          key={loginSelected.id}
          member={loginSelected}
          onClose={() => setLoginSelected(undefined)}
          onChanged={() => {
            setLoginSelected(undefined);
            void members.refetch();
          }}
        />
      )}
      {membershipChange && (
        <SecurityMembershipChange
          key={membershipChange.member.id}
          {...membershipChange}
          onClose={() => setMembershipChange(undefined)}
          onChanged={() => {
            setMembershipChange(undefined);
            void members.refetch();
          }}
        />
      )}
    </div>
  );
}
