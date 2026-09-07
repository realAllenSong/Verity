"use client";

import { useRef, useState } from "react";
import {
  ArrowRightIcon,
  CheckCircleIcon,
  FileArrowUpIcon,
  FileCsvIcon,
  FileJsIcon,
  LockKeyIcon,
  XIcon,
} from "@phosphor-icons/react";
import { Dialog } from "@radix-ui/themes";
import type { BatchSummary, EvidenceRecord, PipelineStage, WorkspaceData } from "@/lib/contracts";

export type DialogName = "step" | "add-data" | null;

function ModalShell({ open, onOpenChange, title, description, children, width = "760px" }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  children: React.ReactNode;
  width?: string;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="verity-dialog" maxWidth={width}>
        <div className="dialog-heading">
          <div><Dialog.Title>{title}</Dialog.Title><Dialog.Description>{description}</Dialog.Description></div>
          <Dialog.Close><button className="icon-button" type="button" aria-label="Close"><XIcon size={18} /></button></Dialog.Close>
        </div>
        {children}
      </Dialog.Content>
    </Dialog.Root>
  );
}

function parseCsv(text: string): Record<string, unknown>[] {
  const lines = text.trim().split(/\r?\n/).filter(Boolean);
  if (lines.length < 2) return [];
  const headers = lines[0].split(",").map((item) => item.trim());
  return lines.slice(1).map((line) => Object.fromEntries(headers.map((header, index) => [header, line.split(",")[index]?.trim() ?? ""])));
}

async function parseFile(file: File): Promise<Record<string, unknown>[]> {
  const text = await file.text();
  if (file.name.endsWith(".jsonl")) return text.split(/\r?\n/).filter(Boolean).map((line) => JSON.parse(line) as Record<string, unknown>);
  if (file.name.endsWith(".csv")) return parseCsv(text);
  const parsed = JSON.parse(text) as unknown;
  if (Array.isArray(parsed)) return parsed as Record<string, unknown>[];
  return [parsed as Record<string, unknown>];
}

function previewValue(row: Record<string, unknown>) {
  const nested = row.payload;
  const data = nested && typeof nested === "object" && !Array.isArray(nested) ? nested as Record<string, unknown> : row;
  for (const key of ["content", "message", "body", "text", "title", "subject", "name"]) {
    const value = data[key];
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  const candidate = Object.entries(data).find(([, value]) => typeof value === "string" || typeof value === "number");
  return candidate ? `${candidate[0]}: ${String(candidate[1])}` : "Structured record";
}

export function AddDataDialog({ open, onOpenChange, datasetId, onStaged }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  datasetId: string;
  onStaged: (batch: BatchSummary) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [rows, setRows] = useState<Record<string, unknown>[]>([]);
  const [error, setError] = useState("");
  const [state, setState] = useState<"idle" | "parsing" | "staging" | "done">("idle");
  const fields = [...new Set(rows.flatMap((row) => Object.keys(row)))];

  async function choose(nextFile: File | undefined) {
    if (!nextFile) return;
    setFile(nextFile); setState("parsing"); setError("");
    try {
      const parsed = await parseFile(nextFile);
      if (!parsed.length) throw new Error("No records found");
      setRows(parsed.slice(0, 10_000)); setState("idle");
    } catch {
      setRows([]); setError("This file could not be parsed. Use JSON, JSONL, or a simple CSV."); setState("idle");
    }
  }

  async function stage() {
    if (!file || !rows.length) return;
    setState("staging");
    const apiUrl = process.env.NEXT_PUBLIC_API_URL;
    try {
      if (apiUrl) {
        const response = await fetch(`${apiUrl}/api/v1/datasets/${datasetId}/batches`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": `${file.name}:${file.size}:${file.lastModified}`,
          },
          body: JSON.stringify({ filename: file.name, records: rows }),
        });
        if (!response.ok) throw new Error("stage failed");
        const payload = await response.json() as { batch: BatchSummary };
        onStaged(payload.batch);
      } else {
        onStaged({ id: `batch_${Date.now()}`, filename: file.name, added_at: new Date().toISOString(), record_count: rows.length, field_count: fields.length, state: "staged" });
      }
      setState("done");
    } catch {
      setError("The local service is unavailable. Start the API and try again."); setState("idle");
    }
  }

  return (
    <ModalShell open={open} onOpenChange={onOpenChange} title="Add data" description="Stage one batch now. Add more batches whenever they arrive." width="680px">
      <div className="upload-body">
        {!file ? (
          <button className="upload-dropzone" type="button" onClick={() => inputRef.current?.click()}>
            <FileArrowUpIcon size={28} /><strong>Choose a data file</strong><span>JSON, JSONL, or CSV · up to 10,000 records in this preview</span>
          </button>
        ) : (
          <div className="file-preview">
            <div className="file-preview-heading"><span className="file-glyph">{file.name.endsWith(".csv") ? <FileCsvIcon size={22} /> : <FileJsIcon size={22} />}</span><span><strong>{file.name}</strong><small>{state === "parsing" ? "Reading..." : `${rows.length.toLocaleString()} records · ${fields.length} fields`}</small></span><button type="button" onClick={() => { setFile(null); setRows([]); setState("idle"); }}>Replace</button></div>
            {rows.length ? <div className="raw-file-preview"><div className="raw-preview-caption"><span>Raw preview</span><small>First {Math.min(3, rows.length)} records</small></div>{rows.slice(0, 3).map((row, index) => <div className="raw-preview-row" key={index}><code>{String(index + 1).padStart(2, "0")}</code><span><strong>{previewValue(row)}</strong><small>{Object.keys(row).slice(0, 5).join(" · ")}</small></span></div>)}</div> : null}
            {rows.length ? <details className="field-disclosure"><summary>{fields.length} detected fields</summary><div className="field-list">{fields.slice(0, 12).map((field) => <code key={field}>{field}</code>)}{fields.length > 12 ? <small>+{fields.length - 12}</small> : null}</div></details> : null}
            <div className="upload-boundary"><LockKeyIcon size={17} /><span>The file stays in this local workspace. Staging does not run a recipe or publish an output.</span></div>
          </div>
        )}
        <input ref={inputRef} hidden type="file" accept=".json,.jsonl,.csv,application/json,text/csv" onChange={(event) => void choose(event.target.files?.[0])} />
        {error ? <p className="form-error">{error}</p> : null}
      </div>
      <div className="dialog-footer">
        {state === "done" ? <Dialog.Close><button className="primary-button" type="button"><CheckCircleIcon size={17} />Done</button></Dialog.Close> : <><Dialog.Close><button className="secondary-button" type="button">Cancel</button></Dialog.Close><button className="primary-button" type="button" disabled={!rows.length || state === "staging"} onClick={() => void stage()}>{state === "staging" ? "Staging..." : <>Stage batch <ArrowRightIcon size={16} /></>}</button></>}
      </div>
    </ModalShell>
  );
}

export function RecordDialog({ record, open, onOpenChange }: { record: EvidenceRecord | null; open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <ModalShell open={open} onOpenChange={onOpenChange} title={record ? record.id.replace("evt_", "rec_") : "Record"} description="Transformation and decision evidence for one record." width="820px">
      {record ? <div className="record-dialog-body">
        <div className="record-compare"><div><span>Before</span><pre>{JSON.stringify(record.before_fields, null, 2)}</pre></div><div className="proposed"><span>After</span><pre>{JSON.stringify(record.after_fields, null, 2)}</pre></div></div>
        <dl className="property-list compact"><div><dt>Reason</dt><dd>{record.reason}</dd></div><div><dt>Confidence</dt><dd>{record.confidence.toFixed(2)}</dd></div><div><dt>Quality</dt><dd>{record.quality_score.toFixed(2)}</dd></div><div><dt>Batch</dt><dd><code>{record.batch_id}</code></dd></div></dl>
        <div className="dialog-note"><LockKeyIcon size={18} /><span>{record.privacy}. Optional origin metadata is not required by the platform.</span></div>
      </div> : null}
    </ModalShell>
  );
}

export function StepDialog({ workspace, stage, open, onOpenChange }: { workspace: WorkspaceData; stage: PipelineStage; open: boolean; onOpenChange: (open: boolean) => void }) {
  const retained = stage.input_count ? (stage.count / stage.input_count) * 100 : 100;
  return (
    <ModalShell open={open} onOpenChange={onOpenChange} title={stage.label} description={stage.description}>
      <div className="step-summary"><div><span>Input</span><strong>{stage.input_count.toLocaleString()}</strong></div><div><span>Output</span><strong>{stage.count.toLocaleString()}</strong></div><div><span>Retained</span><strong>{retained.toFixed(1)}%</strong></div></div>
      <dl className="property-list"><div><dt>Operator</dt><dd><code>{stage.operator}</code></dd></div><div><dt>Version</dt><dd>{workspace.step_settings.version}</dd></div><div><dt>Policy</dt><dd>{workspace.step_settings.policy}</dd></div><div><dt>Threshold</dt><dd>{workspace.step_settings.threshold}</dd></div><div><dt>Snapshot</dt><dd><code>{workspace.step_settings.output_snapshot}</code></dd></div></dl>
    </ModalShell>
  );
}
