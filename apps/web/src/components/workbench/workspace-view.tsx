"use client";

import { RecordBrowser } from "./record-browser";

import { useEffect, useState } from "react";
import {
  CheckCircleIcon,
  DownloadSimpleIcon,
  LockKeyIcon,
  ShieldCheckIcon,
  XIcon,
} from "@phosphor-icons/react";
import { Dialog } from "@radix-ui/themes";
import type { Decision, EvidenceRecord, StageComparison, WorkspaceData } from "@/lib/contracts";
import { PipelineFlow } from "./pipeline-flow";
import { StageComparisonView } from "./stage-comparison";

export function WorkspaceView({ workspace, selectedStageId, onStage, apiUrl }: {
  workspace: WorkspaceData;
  selectedStageId: string;
  onStage: (id: string) => void;
  apiUrl?: string;
}) {
  const selected = workspace.stages.find((stage) => stage.id === selectedStageId) ?? workspace.stages[0];
  const [comparisonState, setComparisonState] = useState<{ key: string; data: StageComparison | null; error: string }>({ key: "", data: null, error: "" });
  const selectedID = selected?.id ?? "";
  const comparisonKey = `${workspace.run_id}:${selectedID}`;
  const comparison = comparisonState.key === comparisonKey ? comparisonState.data : null;
  const error = comparisonState.key === comparisonKey ? comparisonState.error : "";
  const loading = Boolean(apiUrl) && comparisonState.key !== comparisonKey;

  useEffect(() => {
    if (!apiUrl || !selectedID) return;
    const controller = new AbortController();
    void fetch(`${apiUrl}/api/v1/stages/${selectedID}/comparison?limit=10`, {
      cache: "no-store",
      signal: controller.signal,
    }).then(async (response) => {
      if (!response.ok) throw new Error(`comparison returned ${response.status}`);
      setComparisonState({ key: comparisonKey, data: await response.json() as StageComparison, error: "" });
    }).catch((failure: unknown) => {
      if (failure instanceof DOMException && failure.name === "AbortError") return;
      setComparisonState({ key: comparisonKey, data: null, error: "This snapshot could not be opened." });
    });
    return () => controller.abort();
  }, [apiUrl, comparisonKey, selectedID]);

  if (!selected) return null;
  const filtered = ["review", "curated"].includes(selected.id) ? 0 : Math.max(0, selected.input_count - selected.count);
  return (
    <div className="data-workspace">
      <section className="flow-section" aria-label="Data pipeline">
        <div className="flow-heading">
          <div>
            <h2>Transformation path</h2>
            <p>Select any stage to see what changed.</p>
          </div>
          {workspace.decision_breakdown.review === 0 ? <span className="all-clear"><CheckCircleIcon weight="fill" /> No review needed</span> : null}
        </div>
        <PipelineFlow stages={workspace.stages} selectedStage={selected.id} onSelect={onStage} isRunning={false} />
      </section>

      <section className="inspection-surface">
        <header className="inspection-heading" data-stage={selected.id}>
          <div><h2>{selected.label}</h2><p>{selected.description}</p></div>
          <dl>
            <div><dt>Input</dt><dd>{selected.input_count.toLocaleString()}</dd></div>
            <div><dt>Output</dt><dd>{selected.count.toLocaleString()}</dd></div>
            {filtered > 0 ? <div data-filtered="true"><dt>Removed</dt><dd>{filtered.toLocaleString()}</dd></div> : null}
          </dl>
        </header>
        <StageComparisonView key={selectedID} comparison={comparison} loading={loading} error={error} browseAction={apiUrl ? <RecordBrowser key={comparisonKey} stage={selectedID} apiUrl={apiUrl} /> : undefined} />
      </section>
    </div>
  );
}

export function ReviewDialog({ records, total, open, busy, error, onRetry, onOpenChange, onDecision }: {
  records: EvidenceRecord[];
  total: number;
  open: boolean;
  busy: boolean;
  error: string;
  onRetry: () => void;
  onOpenChange: (open: boolean) => void;
  onDecision: (recordId: string, decision: Decision) => void;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="verity-dialog review-dialog" maxWidth="760px">
        <div className="dialog-heading">
          <div><Dialog.Title>Review uncertain records</Dialog.Title><Dialog.Description>{total} records need a judgment call.</Dialog.Description></div>
          <Dialog.Close><button className="icon-button" type="button" aria-label="Close"><XIcon /></button></Dialog.Close>
        </div>
        {error ? <div className="review-message" role="alert">{error}<button type="button" onClick={onRetry}>Retry</button></div> : null}
        <div className="review-cards">
          {records.slice(0, 8).map((record) => (
            <article className="review-card" key={record.id}>
              <div><code>{record.id}</code><strong>{record.extracted_signal}</strong><p>{record.reason}</p><details><summary>Inspect evidence</summary><p>{record.raw_event}</p><small>Rule confidence: {Math.round(record.confidence * 100)}%. This is a heuristic, not a calibrated probability.</small></details></div>
              <div className="review-actions"><button type="button" disabled={busy} onClick={() => onDecision(record.id, "accepted")}><CheckCircleIcon />Accept</button><button type="button" disabled={busy} onClick={() => onDecision(record.id, "rejected")}>Exclude</button></div>
            </article>
          ))}
          {!records.length && !error ? <div className="review-empty"><strong>{busy ? "Loading review queue..." : total === 0 ? "Review complete" : "No records loaded"}</strong></div> : null}
          {records.length > 0 ? <p className="review-page-note">Showing {Math.min(8, records.length)} of {total}. Next records appear as you review.</p> : null}
        </div>
        <footer className="review-privacy"><ShieldCheckIcon /><span>Decisions create a new download snapshot. Earlier snapshots stay unchanged.</span></footer>
      </Dialog.Content>
    </Dialog.Root>
  );
}

export function ResultAction({ workspace, apiUrl }: { workspace: WorkspaceData; apiUrl?: string }) {
  const output = workspace.outputs.find((item) => item.format === "parquet") ?? workspace.outputs[0];
  if (!output) return null;
  return (
    <a className="result-link" href={apiUrl ? `${apiUrl}/api/v1/outputs/${output.id}` : "#"} download>
      <DownloadSimpleIcon />Download result
    </a>
  );
}

export function PrivacyNote({ workspace }: { workspace: WorkspaceData }) {
  return <footer className="workspace-privacy"><LockKeyIcon /><span>{workspace.copy_policy.raw}</span></footer>;
}
