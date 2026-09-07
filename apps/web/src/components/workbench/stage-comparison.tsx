"use client";

import { useMemo, useState } from "react";
import { ArrowRightIcon, CheckCircleIcon, EyeIcon, FunnelIcon, LockKeyIcon, XIcon } from "@phosphor-icons/react";
import { Dialog } from "@radix-ui/themes";
import type { StageComparison, StageComparisonSample } from "@/lib/contracts";

function recordBody(record?: Record<string, unknown>) {
  if (!record) return undefined;
  const nested = record.payload;
  return nested && typeof nested === "object" && !Array.isArray(nested)
    ? nested as Record<string, unknown>
    : record;
}

function valueAt(record: Record<string, unknown> | undefined, keys: string[]) {
  const body = recordBody(record);
  for (const key of keys) {
    const value = body?.[key];
    if (typeof value === "string" && value.trim()) return value.trim();
    if (typeof value === "number" || typeof value === "boolean") return String(value);
  }
  return "";
}

function excerpt(record?: Record<string, unknown>) {
  const content = valueAt(record, ["content", "message", "body", "text", "title", "subject", "name"]);
  if (content) return content.length > 116 ? `${content.slice(0, 113)}...` : content;
  const body = recordBody(record) ?? {};
  const first = Object.entries(body).find(([, value]) => typeof value === "string" || typeof value === "number");
  return first ? `${first[0]}: ${String(first[1])}` : "Structured record";
}

function fieldCount(record?: Record<string, unknown>) {
  return Object.keys(recordBody(record) ?? {}).length;
}

function outcomeLabel(outcome: StageComparisonSample["outcome"]) {
  if (outcome === "input") return "Raw input";
  if (outcome === "normalized") return "Normalized";
  if (outcome === "filtered") return "Filtered out";
  if (outcome === "routed") return "Routed here";
  return "Kept";
}

function PropertyGrid({ record }: { record?: Record<string, unknown> }) {
  const body = recordBody(record);
  if (!body) return <p className="empty-record">No record at this boundary.</p>;
  return (
    <dl className="record-property-grid">
      {Object.entries(body).slice(0, 12).map(([key, value]) => (
        <div key={key}><dt>{key}</dt><dd>{typeof value === "object" ? JSON.stringify(value) : String(value)}</dd></div>
      ))}
    </dl>
  );
}

function ComparisonDialog({ sample, open, onOpenChange }: { sample: StageComparisonSample | null; open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="verity-dialog comparison-dialog" maxWidth="900px">
        <div className="dialog-heading">
          <div><Dialog.Title>{sample?.record_id ?? "Record"}</Dialog.Title><Dialog.Description>One record across this pipeline boundary.</Dialog.Description></div>
          <Dialog.Close><button className="icon-button" type="button" aria-label="Close"><XIcon size={18} /></button></Dialog.Close>
        </div>
        {sample ? (
          <div className="comparison-dialog-body">
            <div className="comparison-callout" data-outcome={sample.outcome}>
              {sample.outcome === "filtered" ? <FunnelIcon /> : <CheckCircleIcon weight="fill" />}
              <strong>{outcomeLabel(sample.outcome)}</strong>
              <span>{sample.reason || (sample.changed_fields?.length ? `Changed ${sample.changed_fields.join(", ")}` : "Record passed this boundary unchanged.")}</span>
            </div>
            <div className="comparison-columns">
              {sample.before ? <section><span>Before</span><PropertyGrid record={sample.before} /></section> : null}
              <section data-current="true"><span>{sample.before ? "After" : "Record"}</span><PropertyGrid record={sample.after} /></section>
            </div>
            <details className="raw-disclosure"><summary>View source JSON</summary><pre>{JSON.stringify({ before: sample.before, after: sample.after }, null, 2)}</pre></details>
          </div>
        ) : null}
      </Dialog.Content>
    </Dialog.Root>
  );
}

export function StageComparisonView({ comparison, loading, error }: { comparison: StageComparison | null; loading: boolean; error: string }) {
  const [filter, setFilter] = useState<"all" | "filtered">("all");
  const [selected, setSelected] = useState<StageComparisonSample | null>(null);
  const hasFiltered = comparison?.samples.some((sample) => sample.outcome === "filtered") ?? false;
  const samples = useMemo(() => comparison?.samples.filter((sample) => filter === "all" || sample.outcome === "filtered") ?? [], [comparison, filter]);

  if (!comparison && loading) return <div className="stage-loading" role="status"><i /><span>Reading this stage artifact...</span></div>;
  if (!comparison) return <div className="empty-state"><strong>Stage artifact unavailable.</strong><span>{error || "Run the pipeline to create this snapshot."}</span></div>;

  return (
    <div className="stage-records" data-loading={loading}>
      <div className="stage-record-toolbar">
        <span>Representative records from the current run</span>
        {hasFiltered ? <div className="view-toggle"><button type="button" data-active={filter === "all"} onClick={() => setFilter("all")}>All</button><button type="button" data-active={filter === "filtered"} onClick={() => setFilter("filtered")}>Filtered out</button></div> : null}
      </div>
      <div className="stage-record-table-wrap">
        <table className="stage-record-table">
          <thead><tr><th>Record</th><th>What the data says</th><th>Result</th><th>Why</th><th><span className="sr-only">Inspect</span></th></tr></thead>
          <tbody>
            {samples.map((sample) => {
              const record = sample.after ?? sample.before;
              const context = valueAt(record, ["kind", "type", "event_type", "status", "state"]);
              const reason = sample.reason || (sample.outcome === "input" ? "Captured as received" : sample.changed_fields?.length ? `Changed ${sample.changed_fields.slice(0, 3).join(", ")}` : "Passed unchanged");
              return (
                <tr key={`${sample.record_id}-${sample.outcome}`} onClick={() => setSelected(sample)} tabIndex={0} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") setSelected(sample); }}>
                  <td><code>{sample.record_id}</code><small>{fieldCount(record)} fields</small></td>
                  <td><strong>{excerpt(record)}</strong>{context ? <small>{context.replaceAll("_", " ")}</small> : null}</td>
                  <td><span className="outcome-label" data-outcome={sample.outcome}>{sample.outcome === "filtered" ? <FunnelIcon /> : <CheckCircleIcon weight="fill" />}{outcomeLabel(sample.outcome)}</span></td>
                  <td><span className="reason-copy">{reason}</span></td>
                  <td><button className="row-inspect" type="button" aria-label={`Inspect ${sample.record_id}`}><EyeIcon /><ArrowRightIcon /></button></td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <footer className="sample-note"><LockKeyIcon />Showing {samples.length} sampled records. Full artifacts stay in the local workspace.</footer>
      <ComparisonDialog sample={selected} open={Boolean(selected)} onOpenChange={(open) => { if (!open) setSelected(null); }} />
    </div>
  );
}
