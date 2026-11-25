import { useMutation } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ErrorBanner } from "@/features/identity/shared";
import { studio, type CatalogKind } from "./api";

export function CatalogPlanDialog({
  project,
  kind,
  titleKey,
  close,
  saved,
}: {
  project: number;
  kind: CatalogKind;
  titleKey: "planCapability" | "planWorkflow" | "planPlugin";
  close: () => void;
  saved: (reference: string) => Promise<void>;
}) {
  const { t } = useTranslation("studio");
  const creation = useMutation({
    mutationFn: (fields: {
      title: string;
      catalogTarget: string;
      checks: string;
    }) => studio.addItem({ ...fields, project, kind }),
  });
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !creation.isPending) close();
      }}
    >
      <DialogContent className="max-h-[88dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t(titleKey)}</DialogTitle>
          <DialogDescription>{t("planCatalogHint")}</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={async (event) => {
            event.preventDefault();
            const fields = new FormData(event.currentTarget);
            const title = String(fields.get("title")).trim();
            const catalogTarget = String(fields.get("reference")).trim();
            const checks = String(fields.get("checks")).trim();
            try {
              await creation.mutateAsync({ title, catalogTarget, checks });
              await saved(catalogTarget);
              close();
            } catch {
              // The mutation error is displayed below.
            }
          }}
        >
          <div className="grid gap-2">
            <Label htmlFor="studio-catalog-title">{t("taskTitle")}</Label>
            <Input
              id="studio-catalog-title"
              name="title"
              required
              maxLength={200}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="studio-catalog-reference">
              {t("catalogTarget")}
            </Label>
            <Input
              id="studio-catalog-reference"
              name="reference"
              placeholder={t("catalogTargetPlaceholder")}
              required
              maxLength={180}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="studio-catalog-checks">
              {t("catalogDevelopmentRequirements")}
            </Label>
            <Textarea
              id="studio-catalog-checks"
              name="checks"
              rows={5}
              maxLength={12000}
            />
          </div>
          {creation.error && <ErrorBanner error={creation.error} />}
          <DialogFooter>
            <Button type="submit" disabled={creation.isPending}>
              <Plus />
              {t("saveCatalogPlan")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
