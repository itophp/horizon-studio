import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { LoaderCircle } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  Combobox,
  ComboboxContent,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox";
import { useSession } from "@/features/identity/auth";
import { studio } from "./api";

type ParentOption = { id: number; title: string };

// Server-backed parent picker. It never filters a full local list: the input
// value only drives a debounced `studio.requirement.search`, and stale replies
// are discarded by the query key plus AbortSignal.
export function RequirementParentPicker({
  id,
  project,
  value,
  valueTitle,
  exclude,
  disabled,
  onChange,
}: {
  id: string;
  project: number;
  value: number;
  valueTitle?: string;
  exclude?: number;
  disabled?: boolean;
  onChange: (value: number) => void;
}) {
  const { t } = useTranslation("studio");
  const profile = useSession();
  const scope = [
    profile.tenantId,
    profile.account.id,
    profile.identityBindingId,
  ];
  const [open, setOpen] = useState(false);
  const [keyword, setKeyword] = useState("");
  const [debounced, setDebounced] = useState("");
  const composing = useRef(false);
  useEffect(() => {
    if (composing.current) return;
    const timer = window.setTimeout(() => setDebounced(keyword.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [keyword]);
  const search = useQuery({
    queryKey: [
      "studio",
      ...scope,
      "requirement-search",
      project,
      exclude ?? 0,
      debounced,
    ],
    queryFn: ({ signal }) =>
      studio.searchRequirements(project, debounced, exclude, signal),
    enabled: open,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });
  const rootOption = useMemo<ParentOption>(
    () => ({ id: 0, title: t("noParentRequirement") }),
    [t],
  );
  const selected: ParentOption = useMemo(() => {
    if (value === 0) return rootOption;
    return { id: value, title: valueTitle || t("requirementUnavailable") };
  }, [rootOption, t, value, valueTitle]);
  const options = useMemo(() => {
    const list: ParentOption[] = [rootOption];
    for (const row of search.data ?? []) {
      if (!list.some((option) => option.id === row.id)) list.push(row);
    }
    if (
      selected.id !== 0 &&
      !list.some((option) => option.id === selected.id)
    ) {
      list.push(selected);
    }
    return list;
  }, [rootOption, search.data, selected]);
  return (
    <Combobox
      items={options}
      filter={null}
      value={selected}
      itemToStringLabel={(item) => item.title}
      itemToStringValue={(item) => String(item.id)}
      isItemEqualToValue={(item, other) => item?.id === other?.id}
      inputValue={open ? keyword : selected.title}
      onInputValueChange={(next, details) => {
        if (!open || composing.current || details.reason !== "input-change") {
          return;
        }
        setKeyword(next);
      }}
      onValueChange={(item) => {
        if (!item) return;
        onChange(item.id);
        setKeyword("");
        setOpen(false);
      }}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) setKeyword("");
      }}
    >
      <ComboboxInput
        id={id}
        aria-label={t("parentRequirement")}
        placeholder={t("searchParentPlaceholder")}
        disabled={disabled}
        showClear={false}
        onCompositionStart={() => {
          composing.current = true;
        }}
        onCompositionEnd={(event) => {
          composing.current = false;
          setKeyword(event.currentTarget.value);
        }}
      />
      <ComboboxContent>
        {search.isError ? (
          <div className="space-y-1 p-2 text-sm">
            <p className="text-destructive">{t("requirementSearchFailed")}</p>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => void search.refetch()}
            >
              {t("retrySearch")}
            </Button>
          </div>
        ) : search.isPending && open ? (
          <p className="flex items-center gap-2 p-2 text-sm text-muted-foreground">
            <LoaderCircle className="size-4 motion-safe:animate-spin" />
            {t("searchingRequirements")}
          </p>
        ) : (
          <>
            <ComboboxList>
              {(item: ParentOption) => (
                <ComboboxItem key={item.id} value={item}>
                  {item.title}
                </ComboboxItem>
              )}
            </ComboboxList>
            {search.isSuccess &&
              keyword.trim() !== "" &&
              search.data.length === 0 && (
                <p
                  role="status"
                  className="px-2 py-1 text-xs text-muted-foreground"
                >
                  {t("noRequirementMatches")}
                </p>
              )}
          </>
        )}
      </ComboboxContent>
    </Combobox>
  );
}
