import { recordBody } from "./record-presentation";
import type { StageComparisonSample } from "@/lib/contracts";

export type Change = "input" | "changed" | "removed" | "added" | "unchanged" | "elsewhere";
export type TableRecord = Omit<StageComparisonSample, "outcome"> & { outcome: string; ordinal: number; change: Change };
export type TablePage = { run_id: string; stage_id: string; snapshot: string; previous_stage_id?: string; rows: number; fields: string[]; counts: Record<string, number>; records: TableRecord[]; next_cursor?: string; scanned: number };
export type View = "changes" | "before" | "after";
export type TextDiffPart = { kind: "same" | "removed" | "added"; text: string };

// Sort object keys recursively so order alone never becomes a data change.
export function stableValue(value: unknown): string {
  if (value === undefined) return "∅";
  if (value === null) return "null";
  if (Array.isArray(value)) return `[${value.map(stableValue).join(", ")}]`;
  if (typeof value === "object") return `{${Object.entries(value).sort(([a], [b]) => a.localeCompare(b)).map(([k, v]) => `${JSON.stringify(k)}: ${stableValue(v)}`).join(", ")}}`;
  return typeof value === "string" ? value : String(value);
}

export function cellChange(row: TableRecord, field: string) {
  const before = recordBody(row.before), after = recordBody(row.after);
  const had = Boolean(before && Object.hasOwn(before, field));
  const has = Boolean(after && Object.hasOwn(after, field));
  const left = before?.[field], right = after?.[field];
  const state = row.change === "input" ? "same" : !had && has ? "added" : had && !has ? "removed" : had && has && fingerprint(left) !== fingerprint(right) ? "changed" : "same";
  return { had, has, left, right, state };
}

function fingerprint(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(fingerprint).join(",")}]`;
  if (value && typeof value === "object") return `{${Object.entries(value).sort(([a], [b]) => a.localeCompare(b)).map(([k, v]) => `${JSON.stringify(k)}:${fingerprint(v)}`).join(",")}}`;
  return JSON.stringify(value) ?? "undefined";
}

const priority = ["content", "message", "text", "body", "title", "subject", "kind", "type", "status", "signal_type", "extracted_signal", "quality_score", "decision", "confidence", "actor", "occurred_at"];
const technical = ["metadata", "content_fingerprint", "dataset_id", "batch_id", "event_id", "record_id", "ingested_at"];
export function orderedFields(fields: string[]) {
  return [...fields].sort((a, b) => {
    const rank = (key: string) => priority.includes(key) ? priority.indexOf(key) : technical.includes(key) ? 1000 : 100;
    return rank(a) - rank(b) || a.localeCompare(b);
  });
}

export function visibleRows(rows: TableRecord[], view: View) {
  return rows.filter(row => view === "changes" || (view === "before" ? row.before : row.after));
}

export function groupRows(rows: TableRecord[], field: string, view: View) {
  if (!field) return [{ label: "", rows }];
  const groups = new Map<string, TableRecord[]>();
  for (const row of rows) {
    const body = recordBody(view === "before" ? row.before : row.after ?? row.before);
    const label = body && Object.hasOwn(body, field) ? stableValue(body[field]) : "∅ Missing";
    if (!groups.has(label)) groups.set(label, []);
    groups.get(label)!.push(row);
  }
  return [...groups].map(([label, rows]) => ({ label, rows }));
}

const prosePriority = [
  "content", "message", "text", "body", "prompt", "response", "summary",
  "description", "title", "subject", "comment", "reason", "notes", "detail",
];

/**
 * Find fields that can be read as prose. This intentionally uses the observed
 * values as well as field names: a general-purpose source may call its text
 * `payload` or `value`, and it should still get a readable presentation.
 */
export function textFields(fields: string[], rows: TableRecord[]) {
  const bodies = rows.flatMap((row) => [recordBody(row.before), recordBody(row.after)]).filter(Boolean) as Record<string, unknown>[];
  return [...fields].filter((field) => {
    const values = bodies.map((body) => body[field]).filter((value): value is string => typeof value === "string" && value.trim().length > 0);
    return values.some((value) => value.trim().length >= 24) || prosePriority.includes(field.toLowerCase());
  }).sort((a, b) => {
    const left = prosePriority.indexOf(a.toLowerCase()), right = prosePriority.indexOf(b.toLowerCase());
    return (left < 0 ? 100 : left) - (right < 0 ? 100 : right) || a.localeCompare(b);
  }).slice(0, 4);
}

function proseTokens(text: string) {
  return text.match(/\s+|[\p{L}\p{N}_]+|[^\s\p{L}\p{N}_]/gu) ?? [];
}

/** A bounded word-level diff for the inline reader. Long values fall back to a single changed span. */
export function diffText(before: string, after: string): TextDiffPart[] {
  if (before === after) return [{ kind: "same", text: after }];
  const a = proseTokens(before), b = proseTokens(after);
  if (a.length > 260 || b.length > 260) return [{ kind: "removed", text: before }, { kind: "added", text: after }];
  const matrix = Array.from({ length: a.length + 1 }, () => new Uint16Array(b.length + 1));
  for (let i = a.length - 1; i >= 0; i--) for (let j = b.length - 1; j >= 0; j--) matrix[i][j] = a[i] === b[j] ? matrix[i + 1][j + 1] + 1 : Math.max(matrix[i + 1][j], matrix[i][j + 1]);
  const parts: TextDiffPart[] = [];
  const add = (kind: TextDiffPart["kind"], text: string) => {
    if (!text) return;
    const previous = parts.at(-1);
    if (previous?.kind === kind) previous.text += text;
    else parts.push({ kind, text });
  };
  let i = 0, j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) { add("same", a[i]); i++; j++; }
    else if (matrix[i + 1][j] >= matrix[i][j + 1]) { add("removed", a[i]); i++; }
    else { add("added", b[j]); j++; }
  }
  while (i < a.length) add("removed", a[i++]);
  while (j < b.length) add("added", b[j++]);
  return parts;
}
