import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { IdentityPolicySearchContextsOutput } from "@/api/generated/capability-identity";
import { identityPolicySearchContexts } from "@/lib/capabilities";
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox";
import { ErrorBanner } from "./shared";

export type PolicyContext = IdentityPolicySearchContextsOutput["items"][number];

const contextKey = (context: PolicyContext) => String(context.accountId);

export function PolicyContextSelect({
  queryKey,
  value,
  onChange,
}: {
  queryKey: readonly unknown[];
  value: PolicyContext | null;
  onChange: (value: PolicyContext | null) => void;
}) {
  const { t, i18n } = useTranslation();
  const [text, setText] = useState("");
  const [keyword, setKeyword] = useState("");
  const [open, setOpen] = useState(false);
  const [composing, setComposing] = useState(false);
  const term = text.trim();
  useEffect(() => {
    if (!open || composing) return;
    const timer = setTimeout(() => setKeyword(term), 300);
    return () => clearTimeout(timer);
  }, [term, open, composing]);
  const ready = open && !composing && !!keyword && keyword === term;
  const results = useQuery({
    queryKey: [...queryKey, "simulation-contexts", i18n.language, keyword],
    queryFn: ({ signal }) => identityPolicySearchContexts({ keyword }, signal),
    enabled: ready,
    retry: false,
  });
  const label = (item: PolicyContext) =>
    t("admin:policy.contextOption", {
      name: item.fullName,
      username: item.username,
    });
  return (
    <div className="space-y-1">
      <Combobox
        items={ready ? (results.data?.data.items ?? []) : []}
        value={value}
        onValueChange={onChange}
        inputValue={text}
        onInputValueChange={setText}
        open={open}
        onOpenChange={setOpen}
        filter={null}
        itemToStringLabel={label}
        itemToStringValue={contextKey}
        isItemEqualToValue={(a, b) => contextKey(a) === contextKey(b)}
      >
        <ComboboxInput
          aria-label={t("admin:policy.subject")}
          placeholder={t("admin:policy.searchContext")}
          maxLength={100}
          onCompositionStart={() => setComposing(true)}
          onCompositionEnd={() => setComposing(false)}
          showClear
        />
        <ComboboxContent>
          <ComboboxEmpty>
            {t(
              !term
                ? "admin:policy.searchContext"
                : !ready || results.isFetching
                  ? "admin:policy.searchingContext"
                  : results.error
                    ? "admin:policy.contextSearchFailed"
                    : "admin:policy.noContexts",
            )}
          </ComboboxEmpty>
          <ComboboxList>
            {(item: PolicyContext) => (
              <ComboboxItem key={contextKey(item)} value={item}>
                {label(item)}
              </ComboboxItem>
            )}
          </ComboboxList>
        </ComboboxContent>
      </Combobox>
      {ready && results.error && (
        <ErrorBanner
          error={results.error}
          onRetry={() => void results.refetch()}
        />
      )}
    </div>
  );
}
