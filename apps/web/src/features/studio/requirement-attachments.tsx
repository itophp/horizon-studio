import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Download,
  LoaderCircle,
  Paperclip,
  Trash2,
  Upload,
} from "lucide-react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { useSession } from "@/features/identity/auth";
import { ErrorBanner, Loading } from "@/features/identity/shared";
import { downloadContent } from "@/features/governance/shared";
import { filesContent, filesUpload } from "@/lib/capabilities";
import { studio, type Requirement, type RequirementAttachment } from "./api";

const MAX_BYTES = 20 * 1024 * 1024;

function toBase64(bytes: Uint8Array) {
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 8192) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192));
  }
  return btoa(binary);
}

export function RequirementAttachments({
  project,
  requirement,
  canWrite,
}: {
  project: number;
  requirement: Requirement;
  canWrite: boolean;
}) {
  const { t, i18n } = useTranslation("studio");
  const profile = useSession();
  const scope = [
    profile.tenantId,
    profile.account.id,
    profile.identityBindingId,
  ];
  const client = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const canUpload =
    canWrite &&
    !requirement.archived &&
    profile.permissions.includes("files:create");
  const canDownload = profile.permissions.includes("files:read");
  const queryKey = [
    "studio",
    ...scope,
    "requirement-attachments",
    project,
    requirement.id,
  ];
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) =>
      studio.requirementAttachments(project, requirement.id, signal),
    retry: false,
  });
  const remove = useMutation({
    mutationFn: (attachment: RequirementAttachment) =>
      studio.removeRequirementAttachment(
        project,
        requirement.id,
        attachment.fileId,
      ),
    onSuccess: () => {
      setError(null);
      void client.invalidateQueries({ queryKey });
    },
  });
  const refresh = () => client.invalidateQueries({ queryKey });
  const onFile = async (file: File) => {
    setError(null);
    setNotice(null);
    if (file.size > MAX_BYTES) {
      setNotice(t("attachmentTooLarge"));
      return;
    }
    setBusy(true);
    try {
      const created = await filesUpload({
        name: file.name,
        purpose: "attachment",
        content: toBase64(new Uint8Array(await file.arrayBuffer())),
      });
      await studio.addRequirementAttachment(
        project,
        requirement.id,
        created.data.id,
      );
      await refresh();
    } catch (value) {
      setError(value);
    } finally {
      setBusy(false);
      if (input.current) input.current.value = "";
    }
  };
  const download = async (attachment: RequirementAttachment) => {
    setError(null);
    setNotice(null);
    try {
      const result = await filesContent(attachment.fileId);
      if (result.status !== 200) return;
      const bytes = Uint8Array.from(atob(result.data.content), (character) =>
        character.charCodeAt(0),
      );
      downloadContent(result.data.name, bytes, result.data.mime);
    } catch (value) {
      setError(value);
    }
  };
  const attachments = query.data ?? [];
  const pending = remove.isPending || busy;
  return (
    <section className="space-y-3 border-t pt-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-medium">
          {t("attachments")}
          {attachments.length > 0 && (
            <span className="ml-1 text-muted-foreground">
              {t("attachmentCount", { count: attachments.length })}
            </span>
          )}
        </h3>
        {canUpload && (
          <>
            <input
              ref={input}
              type="file"
              className="hidden"
              onChange={(event) => {
                const file = event.currentTarget.files?.[0];
                if (file) void onFile(file);
              }}
            />
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={pending}
              onClick={() => input.current?.click()}
            >
              {busy ? (
                <LoaderCircle className="motion-safe:animate-spin" />
              ) : (
                <Upload />
              )}
              {t("addAttachment")}
            </Button>
          </>
        )}
      </div>
      {canWrite &&
        !requirement.archived &&
        !profile.permissions.includes("files:create") && (
          <p className="text-xs text-muted-foreground">
            {t("attachmentUploadPermission")}
          </p>
        )}
      {!canDownload && (
        <p className="text-xs text-muted-foreground">
          {t("attachmentReadPermission")}
        </p>
      )}
      {query.isPending ? (
        <Loading />
      ) : query.error ? (
        <ErrorBanner error={query.error} onRetry={() => void query.refetch()} />
      ) : attachments.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("noAttachments")}</p>
      ) : (
        <ul className="grid gap-2">
          {attachments.map((attachment) => (
            <li
              key={attachment.fileId}
              className="flex items-center gap-2 rounded-lg border p-2"
            >
              <Paperclip
                className="size-4 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
              <span className="min-w-0 flex-1">
                <span
                  className="block truncate text-sm"
                  title={attachment.name}
                >
                  {attachment.name}
                </span>
                <span className="block truncate text-xs text-muted-foreground">
                  {new Intl.NumberFormat(i18n.resolvedLanguage, {
                    style: "unit",
                    unit: "byte",
                    unitDisplay: "short",
                    maximumFractionDigits: 1,
                  }).format(attachment.size)}
                  {attachment.mime ? ` · ${attachment.mime}` : ""}
                </span>
              </span>
              {canDownload && (
                <Button
                  type="button"
                  size="icon-sm"
                  variant="ghost"
                  aria-label={t("downloadAttachment")}
                  title={t("downloadAttachment")}
                  onClick={() => void download(attachment)}
                >
                  <Download />
                </Button>
              )}
              {canWrite && !requirement.archived && (
                <Button
                  type="button"
                  size="icon-sm"
                  variant="ghost"
                  disabled={pending}
                  aria-label={t("removeAttachment")}
                  title={t("removeAttachment")}
                  onClick={() => remove.mutate(attachment)}
                >
                  <Trash2 />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {notice && (
        <p role="alert" className="text-sm text-destructive">
          {notice}
        </p>
      )}
      {remove.error ? <ErrorBanner error={remove.error} /> : null}
      {error ? <ErrorBanner error={error} /> : null}
    </section>
  );
}
