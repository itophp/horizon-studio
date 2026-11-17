import { useTranslation } from "react-i18next";
import { DetailField } from "./library";

export function PluginManifestFields({
  manifest,
}: {
  manifest: {
    mode: string;
    apiVersion: string;
    extensions?: string[] | null;
    dependencies?: { id: string; version: string }[];
  };
}) {
  const { t } = useTranslation("studio");
  return (
    <>
      <DetailField label={t("pluginMode")}>{manifest.mode}</DetailField>
      <DetailField label={t("pluginAPIVersion")}>
        {manifest.apiVersion}
      </DetailField>
      <DetailField label={t("pluginExtensions")}>
        {manifest.extensions?.join(", ") || t("none")}
      </DetailField>
      <DetailField label={t("pluginDependencies")}>
        {manifest.dependencies
          ?.map((dependency) => `${dependency.id}@${dependency.version}`)
          .join(", ") || t("none")}
      </DetailField>
    </>
  );
}
