import { tr, currentLocale } from "./i18n";
import { hours, today, type Project, type Employee } from "./types";
import { taskStatus, taskExecutors, scheduleAvailable } from "./taskStatus";
const headers = () => [
  tr("mb9ce27191d"),
  tr("m225077c6d4"),
  tr("m4af5d2efad"),
  tr("ma5787a1e68"),
  tr("m3639a4bd5d"),
  tr("m5a5cb3bd27"),
  tr("m037174d588"),
];
export function reportRows(
  project: Project,
  employees: Employee[],
  date = today(),
): string[][] {
  return [
    headers(),
    ...project.tasks.map((t) => [
      `${t.summary ? tr("m4fa412f672") : ""}${t.title}`,
      taskStatus(t, project, date).label,
      scheduleAvailable(t, project) ? t.start : "",
      scheduleAvailable(t, project) ? t.end : "",
      String(hours(t.planned_minutes)),
      String(hours(t.actual_minutes)),
      taskExecutors(t, employees),
    ]),
  ];
}
export function escapeHTML(value: string): string {
  return value.replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ]!,
  );
}
export function reportCSV(
  project: Project,
  employees: Employee[],
  date = today(),
): string {
  return (
    "\ufeff" +
    reportRows(project, employees, date)
      .map((row) =>
        row
          .map(
            (v) =>
              '"' +
              (/^[\s\u0000-\u001f]*[=+@-]/.test(v) || /^[\t\r\n]/.test(v)
                ? "'" + v
                : v
              ).replaceAll('"', '""') +
              '"',
          )
          .join(";"),
      )
      .join("\r\n")
  );
}
export function reportHTML(
  project: Project,
  employees: Employee[],
  date = today(),
): string {
  const [header, ...rows] = reportRows(project, employees, date);
  return (
    '<!doctype html><html lang="' +
    currentLocale() +
    '"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>' +
    escapeHTML(project.title) +
    "</title><style>body{font:14px system-ui,sans-serif;color:#203047;margin:32px}h1{font-size:25px}table{border-collapse:collapse;width:100%}th,td{border:1px solid #ced7e2;padding:8px;text-align:left;vertical-align:top;white-space:pre-wrap;overflow-wrap:anywhere}th{background:#edf3fa}thead{display:table-header-group}tr{break-inside:avoid}p{color:#596b80}@page{size:A4 landscape;margin:14mm}@media print{body{margin:0;font-size:10px}}</style></head><body><h1>" +
    escapeHTML(project.title) +
    "</h1><p>gmVector · " +
    escapeHTML(tr("exportEntireProject")) +
    " · " +
    escapeHTML(date) +
    escapeHTML(tr("mb257fef64d")) +
    project.revision +
    "</p><p>" +
    escapeHTML(project.brief) +
    "</p><table><thead><tr>" +
    header.map((v) => "<th>" + escapeHTML(v) + "</th>").join("") +
    "</tr></thead><tbody>" +
    rows
      .map(
        (row) =>
          "<tr>" +
          row.map((v) => "<td>" + escapeHTML(v) + "</td>").join("") +
          "</tr>",
      )
      .join("") +
    "</tbody></table></body></html>"
  );
}
