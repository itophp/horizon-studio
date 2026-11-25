import type { ReactNode } from "react";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

export { TabsContent } from "@/components/ui/tabs";

export function RuntimeTabs({
  tabs,
  value,
  onValueChange,
  label,
  hideSingleTab,
  wrap,
  children,
}: {
  tabs: { id: string; label: ReactNode }[];
  value: string;
  onValueChange: (value: string) => void;
  label?: string;
  hideSingleTab?: boolean;
  wrap?: boolean;
  children?: ReactNode;
}) {
  return (
    <Tabs
      className="protocol-layout-tabs"
      value={value}
      onValueChange={onValueChange}
    >
      {(!hideSingleTab || tabs.length > 1) && (
        <TabsList
          className={wrap ? "overview-tabs runtime-tabs-wrap" : "overview-tabs"}
          aria-label={label}
        >
          {tabs.map((tab) => (
            <TabsTrigger key={tab.id} value={tab.id}>
              {tab.label}
            </TabsTrigger>
          ))}
        </TabsList>
      )}
      {children}
    </Tabs>
  );
}
