import { tr, currentLocale } from "./i18n";
import { escapeHTML } from "./exportReport";
import { hours } from "./types";
import { planningTimestamp, supportsPlanningZone } from "./planningTime";
import type { HistoryExport, HistoryItem } from "./history";

export const HISTORY_OUTPUT_BYTES = 64 * 1024 * 1024,
  HISTORY_PDF_BYTES = 8 * 1024 * 1024,
  HISTORY_PDF_ROWS = 10000;
export class HistoryConversionLimit extends Error {
  constructor() {
    super("history.conversionLimit");
    this.name = "HistoryConversionLimit";
  }
}
export class BoundedText {
  private chunks: string[] = [];
  private bytes = 0;
  constructor(private limit: number) {}
  add(value: string) {
    const size = new TextEncoder().encode(value).length;
    if (this.bytes + size > this.limit) throw new HistoryConversionLimit();
    this.bytes += size;
    this.chunks.push(value);
  }
  finish() {
    return this.chunks.join("");
  }
}
const csv = (value: string) =>
  '"' +
  (/^[\s\u0000-\u001f]*[=+@-]/.test(value) || /^[\t\r\n]/.test(value)
    ? "'" + value
    : value
  ).replaceAll('"', '""') +
  '"';
const columns = () => [
  tr("m232a0ead18"),
  tr("history.taskID"),
  tr("mb9ce27191d"),
  tr("history.employeeID"),
  tr("mc0b744cf1c"),
  tr("history.minutes"),
  tr("m7e6da98020"),
  tr("md89b310286"),
  tr("history.entryID"),
];
const values = (item: HistoryItem) => [
  item.date,
  item.task_id,
  item.task_title,
  item.employee_id,
  item.employee_name || tr("history.unknownEmployee"),
  String(item.minutes),
  String(hours(item.minutes)),
  item.comment,
  item.id,
];
export function historyReport(
  document: HistoryExport,
  format: "json" | "csv" | "html" | "pdf",
): string {
  const output = new BoundedText(
    format === "pdf" ? HISTORY_PDF_BYTES : HISTORY_OUTPUT_BYTES,
  );
  if (format === "json") {
    const { items, ...metadata } = document;
    output.add(JSON.stringify(metadata).slice(0, -1) + ',"items":[');
    items.forEach((item, index) =>
      output.add((index ? "," : "") + JSON.stringify(item)),
    );
    output.add("]}");
    return output.finish();
  }
  if (format === "pdf" && document.items.length > HISTORY_PDF_ROWS)
    throw new HistoryConversionLimit();
  if (!supportsPlanningZone(document.planning_timezone))
    throw Error(tr("clock.unsupported", { zone: document.planning_timezone }));
  const generated = planningTimestamp(
    document.generated_at,
    document.planning_timezone,
    currentLocale(),
  );
  const metadata = [
    [tr("history.title"), document.project_title],
    [tr("clock.zone"), document.planning_timezone],
    [tr("history.generated"), generated],
    [tr("history.records"), String(document.total_count)],
    [tr("history.recorded"), String(hours(document.recorded_minutes))],
    [tr("history.legacy"), String(hours(document.legacy_unattributed.minutes))],
    [tr("history.legacyNote"), ""],
  ];
  if (format === "csv") {
    output.add("\ufeff");
    for (const row of metadata) output.add(row.map(csv).join(";") + "\r\n");
    output.add(columns().map(csv).join(";") + "\r\n");
    for (const item of document.items)
      output.add(values(item).map(csv).join(";") + "\r\n");
    return output.finish();
  }
  output.add(
    '<!doctype html><html lang="' +
      currentLocale() +
      '"><head><meta charset="utf-8"><title>' +
      escapeHTML(document.project_title) +
      "</title><style>body{font:13px system-ui;color:#203047;margin:24px}table{border-collapse:collapse;width:100%}th,td{border:1px solid #ccd5df;padding:5px;white-space:pre-wrap;overflow-wrap:anywhere;vertical-align:top}thead{display:table-header-group}tr{break-inside:avoid}@page{size:A4 landscape;margin:12mm}@media print{body{font-size:9px;margin:0}}</style></head><body>",
  );
  for (const row of metadata)
    output.add(
      "<p><b>" + escapeHTML(row[0]) + "</b> " + escapeHTML(row[1]) + "</p>",
    );
  output.add(
    "<table><thead><tr>" +
      columns()
        .map((value) => "<th>" + escapeHTML(value) + "</th>")
        .join("") +
      "</tr></thead><tbody>",
  );
  for (const item of document.items)
    output.add(
      "<tr>" +
        values(item)
          .map((value) => "<td>" + escapeHTML(value) + "</td>")
          .join("") +
        "</tr>",
    );
  output.add("</tbody></table></body></html>");
  return output.finish();
}
