import type { ReactNode } from "react";
import { Tabs } from "@base-ui/react/tabs";

export function AdminPageTabs({
  tabs,
  value,
  onValueChange,
  label,
  children,
}: {
  tabs: readonly { id: string; label: string; icon?: ReactNode }[];
  value: string;
  onValueChange: (value: string) => void;
  label?: string;
  children: ReactNode;
}) {
  if (tabs.length < 2) return children;

  return (
    <Tabs.Root
      value={value}
      onValueChange={(next) => {
        if (typeof next === "string") onValueChange(next);
      }}
    >
      <Tabs.List
        className="admin-page-tabs max-w-full overflow-x-auto whitespace-nowrap [&>button]:shrink-0"
        aria-label={label}
      >
        {tabs.map((tab) => (
          <Tabs.Tab
            key={tab.id}
            value={tab.id}
            className={tab.id === value ? "active" : undefined}
          >
            {tab.icon}
            {tab.label}
          </Tabs.Tab>
        ))}
      </Tabs.List>
      <Tabs.Panel value={value}>{children}</Tabs.Panel>
    </Tabs.Root>
  );
}
