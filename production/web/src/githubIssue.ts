/** Public issue draft: an explicit field allowlist, never a diagnostic report. */
export type IssueFields = {
  title: string;
  steps: string;
  expected: string;
  actual: string;
};
export const issueBaseURL = "https://github.com/DrToppi/gm-vector/issues/new";
export function githubIssueDraft(fields: IssueFields, version?: unknown) {
  const title = fields.title.trim().slice(0, 200);
  const safeVersion =
    typeof version === "string" &&
    version.length <= 64 &&
    /^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?(?:\+[a-zA-Z0-9.-]+)?$/.test(version)
      ? version
      : "";
  const body = [
    "## Steps to reproduce",
    fields.steps,
    "",
    "## Expected result",
    fields.expected,
    "",
    "## Actual result",
    fields.actual,
    ...(safeVersion ? ["", "## gmVector version", safeVersion] : []),
  ].join("\n");
  const url = issueBaseURL + "?" + new URLSearchParams({ title, body });
  return { title, body, url, fits: url.length <= 6000 };
}
