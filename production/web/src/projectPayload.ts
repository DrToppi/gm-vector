import type { Project } from "./types";

// Derived ledgers belong in read responses. They must not consume the existing
// write/import body budget or be mistaken for authoritative data on roundtrip.
export function projectPayload(project: Project): Project {
  const {
    loads,
    warnings,
    provisional_years,
    calculation_state,
    ...authoritative
  } = project;
  return authoritative;
}

export function projectWriteBody(path: string, body: unknown): unknown {
  if (path !== "/import" && !/^\/projects\/[^/]+(?:\/preview)?$/.test(path))
    return body;
  if (!body || typeof body !== "object" || !("project" in body)) return body;
  const value = body as { project?: unknown };
  if (
    !value.project ||
    typeof value.project !== "object" ||
    !("tasks" in value.project) ||
    !("calendar_id" in value.project)
  )
    return body;
  return { ...body, project: projectPayload(value.project as Project) };
}
