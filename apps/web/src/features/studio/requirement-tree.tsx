import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import {
  TreeTable,
  type TreeTableColumn,
  type TreeTableRow,
} from "@/components/shared/tree";
import type { Requirement } from "./api";

// Two-level requirement tree. The tree table only renders the hierarchy; the
// detail pane still owns the record, so selecting a row keeps the URL entry
// contract used by the workbench and the project management page.
export function RequirementTree({
  requirements,
  selected,
  onSelect,
}: {
  requirements: Requirement[];
  selected?: string;
  onSelect: (id: string) => void;
}) {
  const { t } = useTranslation("studio");
  // Parents start expanded, so a deep link to a child is visible without an
  // effect that would synchronously rewrite collapse state.
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const rows = useMemo<TreeTableRow<Requirement>[]>(() => {
    const children = new Map<number, Requirement[]>();
    for (const row of requirements) {
      if (!row.parentId) continue;
      const list = children.get(row.parentId) ?? [];
      list.push(row);
      children.set(row.parentId, list);
    }
    const flattened: TreeTableRow<Requirement>[] = [];
    const roots = requirements
      .filter((row) => !row.parentId)
      .sort((left, right) => right.id - left.id);
    for (const root of roots) {
      const kids = (children.get(root.id) ?? []).sort(
        (left, right) => right.id - left.id,
      );
      flattened.push({
        key: String(root.id),
        record: root,
        depth: 0,
        hasChildren: kids.length > 0,
      });
      if (collapsed.has(String(root.id))) continue;
      for (const child of kids) {
        flattened.push({
          key: String(child.id),
          record: child,
          depth: 1,
          hasChildren: false,
        });
      }
    }
    return flattened;
  }, [requirements, collapsed]);
  const columns: TreeTableColumn<Requirement>[] = [
    {
      id: "title",
      header: t("requirementColumn"),
      cell: (row) => (
        <span className="min-w-0 flex-1 truncate text-sm" title={row.title}>
          {row.title}
        </span>
      ),
    },
    {
      id: "status",
      header: t("statusColumn"),
      className: "w-16",
      cell: (row) => (
        <Badge
          variant={row.archived ? "outline" : "secondary"}
          className="h-5 px-1.5 text-[0.65rem] font-normal"
        >
          {t(row.archived ? "requirementArchived" : "requirementActive")}
        </Badge>
      ),
    },
  ];
  return (
    <div className="requirement-tree">
      <TreeTable
        rows={rows}
        columns={columns}
        collapsedKeys={collapsed}
        onToggle={(key) =>
          setCollapsed((current) => {
            const next = new Set(current);
            if (next.has(key)) next.delete(key);
            else next.add(key);
            return next;
          })
        }
        labels={{
          expand: t("expandRequirement"),
          collapse: t("collapseRequirement"),
          range: () => "",
          page: () => "",
          previous: "",
          next: "",
        }}
        showPagination={false}
        selectedKey={selected}
        onSelect={onSelect}
        empty={
          <p className="px-3 py-6 text-center text-sm text-muted-foreground">
            {t("noRequirements")}
          </p>
        }
      />
    </div>
  );
}
