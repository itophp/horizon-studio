export const studioProjectsPath = "/studio-projects";

export function studioProjectPath(project: number, view?: string) {
  const path = `${studioProjectsPath}/${project}`;
  return view ? `${path}?view=${view}` : path;
}

export function studioWorkbenchPath(project: number, task?: number) {
  return `/studio?project=${project}${task ? `&task=${task}` : ""}`;
}
