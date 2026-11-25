import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, LoaderCircle, X } from "lucide-react";
import type { IdentityMembershipListOutput } from "@/api/generated/capability-identity";
import { identityMembershipRevoke } from "@/lib/capabilities";
import { Button } from "@/components/ui/button";
import { EditorDialog, ErrorBanner } from "./shared";

type Member = IdentityMembershipListOutput["items"][number];

export function RevokeMembership({
  member,
  onClose,
  onRevoked,
}: {
  member: Member;
  onClose: () => void;
  onRevoked: () => void;
}) {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const submitting = useRef(false);
  const operation = useRef<AbortController | null>(null);
  const key = useRef(crypto.randomUUID());
  useEffect(() => {
    const controller = new AbortController();
    operation.current = controller;
    return () => controller.abort();
  }, []);
  async function revoke() {
    if (submitting.current) return;
    submitting.current = true;
    setBusy(true);
    setError(undefined);
    const signal = operation.current!.signal;
    try {
      await identityMembershipRevoke(
        { accountId: member.id },
        key.current,
        signal,
      );
      if (!signal.aborted) onRevoked();
    } catch (cause) {
      if (!signal.aborted) setError(cause);
    } finally {
      submitting.current = false;
      if (!signal.aborted) setBusy(false);
    }
  }
  return (
    <EditorDialog
      open
      title={t("admin:security.workspaceMembers.revoke")}
      description={t("admin:security.workspaceMembers.revokeHelp", {
        name: member.username,
        login: member.username,
      })}
      onOpenChange={(open) => {
        if (!open && !submitting.current) onClose();
      }}
    >
      {!!error && <ErrorBanner error={error} />}
      <div className="form-actions">
        <Button
          type="button"
          variant="outline"
          disabled={busy}
          onClick={onClose}
        >
          <X />
          {t("admin:actions.cancel")}
        </Button>
        <Button
          type="button"
          variant="destructive"
          disabled={busy}
          onClick={() => void revoke()}
        >
          {busy ? <LoaderCircle className="animate-spin" /> : <Check />}
          {t("admin:security.confirm")}
        </Button>
      </div>
    </EditorDialog>
  );
}
