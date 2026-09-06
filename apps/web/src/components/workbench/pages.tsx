"use client";

import { useMemo, useState } from "react";
import {
  ArrowDownIcon,
  ArrowRightIcon,
  CheckCircleIcon,
  CircleIcon,
  CodeIcon,
  DownloadSimpleIcon,
  DotsThreeIcon,
  FileIcon,
  FunnelIcon,
  InfoIcon,
  LockKeyIcon,
  PencilSimpleIcon,
  PlayIcon,
  PlusIcon,
  RowsIcon,
  ShieldCheckIcon,
  WarningCircleIcon,
  XCircleIcon,
} from "@phosphor-icons/react";
import type { Decision, EvidenceRecord, PipelineStage, RecipeOperator, RunSummary, WorkspaceData } from "@/lib/contracts";
import { PipelineFlow } from "./pipeline-flow";
import { RecordTable } from "./record-table";
import { DistributionView, SchemaView } from "./stage-views";

type TabName = "records" | "distribution" | "schema";

function PageHeader({ meta, title, description, children }: { meta?: string; title: string; description?: string; children?: React.ReactNode }) {
  return (
    <header className="page-header">
      <div>{meta ? <span className="page-meta">{meta}</span> : null}<h1>{title}</h1>{description ? <p>{description}</p> : null}</div>
      {children ? <div className="page-actions">{children}</div> : null}
    </header>
  );
}

function Status({ state }: { state: string }) {
  const normalized = ["succeeded", "complete", "configured", "published", "ready"].includes(state)
    ? "success"
    : state === "failed"
      ? "failed"
      : state === "warning" || state === "review" || state === "attention"
        ? "warning"
        : "neutral";
  return (
    <span className="status-label" data-state={normalized}>
      {normalized === "success" ? <CheckCircleIcon weight="fill" /> : normalized === "failed" ? <XCircleIcon weight="fill" /> : normalized === "warning" ? <WarningCircleIcon weight="fill" /> : <CircleIcon weight="fill" />}
      {state}
    </span>
  );
}

export function PipelinePage({ workspace, selectedStageId, onStage, onRun, isRunning, onStep, onRecord, onDecision, lastRun }: {
  workspace: WorkspaceData;
  selectedStageId: string;
  onStage: (id: string) => void;
  onRun: () => void;
  isRunning: boolean;
  onStep: () => void;
  onRecord: (record: EvidenceRecord) => void;
  onDecision: (id: string, decision: Decision) => void;
  lastRun: string;
}) {
  const [tab, setTab] = useState<TabName>("records");
  const selected = workspace.stages.find((stage) => stage.id === selectedStageId) ?? workspace.stages[4];
  const visible = useMemo(() => {
    if (selectedStageId === "review") return workspace.records.filter((record) => record.decision === "review");
    if (selectedStageId === "curated") return workspace.records.filter((record) => record.decision !== "review" && record.decision !== "rejected");
    return workspace.records;
  }, [selectedStageId, workspace.records]);
  const passedChecks = selected.checks?.filter((check) => check.state === "passed").length;

  return (
    <>
      <PageHeader meta={`Recipe v${workspace.recipe.version} · Last run ${lastRun}`} title={workspace.dataset.name} description="Turn raw batches into traceable, reviewable datasets.">
        <button className="secondary-button" type="button" onClick={onStep}><FunnelIcon size={17} />Configure</button>
        <button className="primary-button" type="button" onClick={onRun} disabled={isRunning}><PlayIcon weight="fill" size={16} />{isRunning ? "Running…" : "Run pipeline"}</button>
      </PageHeader>

      <PipelineFlow stages={workspace.stages} selectedStage={selectedStageId} onSelect={onStage} />

      <section className="panel stage-panel">
        <div className="panel-heading stage-heading">
          <div><span className="section-kicker">Selected stage</span><h2>{selected.label}</h2><p>{selected.description}</p></div>
          <div className="stage-summary">
            {passedChecks !== undefined ? <span><CheckCircleIcon weight="fill" />{passedChecks}/{selected.checks?.length} checks passed</span> : null}
            <button className="quiet-button" type="button" onClick={onStep}><InfoIcon />Details</button>
          </div>
        </div>
        <div className="tabbar" role="tablist">
          {(["records", "distribution", "schema"] as TabName[]).map((name) => <button key={name} role="tab" aria-selected={tab === name} onClick={() => setTab(name)}>{name[0].toUpperCase() + name.slice(1)}</button>)}
          <span>{selected.count.toLocaleString()} records</span>
        </div>
        {tab === "records" ? <><RecordTable records={visible.slice(0, 7)} selectedRecordId={null} onOpenRecord={onRecord} onDecision={onDecision} /><TablePager shown={Math.min(visible.length, 7)} total={selected.count} /></> : null}
        {tab === "distribution" ? <DistributionView data={workspace.signal_distribution} /> : null}
        {tab === "schema" ? <SchemaView workspace={workspace} /> : null}
      </section>
    </>
  );
}

function TablePager({ shown, total }: { shown: number; total: number }) {
  return <footer className="table-footer"><span>Showing {shown} of {total.toLocaleString()}</span><div><button disabled type="button">Previous</button><strong>1</strong><button type="button">Next</button></div></footer>;
}

export function DataPage({ workspace, onAdd }: { workspace: WorkspaceData; onAdd: () => void }) {
  const { dataset } = workspace;
  const contract = dataset.schema_contract;
  return (
    <>
      <PageHeader title="Data" description="Inspect batches, quality, and the schema contract before a run.">
        <button className="primary-button" type="button" onClick={onAdd}><PlusIcon size={17} />Add data</button>
      </PageHeader>
      <section className="panel dataset-profile">
        <div className="metric-strip">
          <div><strong>{dataset.record_count.toLocaleString()}</strong><span>Records</span></div>
          <div><strong>{dataset.field_count}</strong><span>Fields</span></div>
          <div><strong>{dataset.batch_count}</strong><span>Batches</span></div>
          <div><Status state={dataset.state} /><span>Dataset state</span></div>
        </div>
        <div className="quality-strip"><QualityMetric label="Completeness" value={dataset.completeness} /><QualityMetric label="Validity" value={dataset.validity} /></div>
      </section>
      <div className="data-layout">
        <section className="panel table-panel">
          <div className="panel-heading"><div><h2>Ingestion batches</h2><p>Append-only inputs with independent lineage.</p></div><button className="icon-button" aria-label="Batch options"><DotsThreeIcon /></button></div>
          <div className="plain-table-wrap">
            <table className="plain-table"><thead><tr><th>Batch</th><th>Added</th><th>Records</th><th>Fields</th><th>State</th></tr></thead><tbody>
              {workspace.batches.map((batch) => <tr key={batch.id}><td><span className="file-cell"><FileIcon size={18} /><span><strong>{batch.filename}</strong><small>{batch.id}</small></span></span></td><td>{formatDate(batch.added_at)}</td><td className="mono">{batch.record_count.toLocaleString()}</td><td className="mono">{batch.field_count}</td><td><Status state={batch.state} /></td></tr>)}
            </tbody></table>
          </div>
        </section>
        <aside className="panel schema-rail">
          <div className="panel-heading"><div><h2>Schema contract</h2><p>{dataset.field_count} inferred fields</p></div><CodeIcon /></div>
          {contract ? <dl className="contract-list"><div><dt>Columns</dt><dd>{contract.columns}</dd></div><div><dt>Data types</dt><dd>{contract.data_types}</dd></div><div><dt>Violation</dt><dd>{contract.on_violation.replaceAll("_", " ")}</dd></div></dl> : null}
          <div className="schema-preview">{[...workspace.schema_before, ...workspace.schema_after].slice(0, 5).map((field) => <div className="schema-line" key={field.field}><code>{field.field}</code><span>{field.type}</span></div>)}</div>
          <button className="text-link" type="button">View all fields <ArrowRightIcon /></button>
        </aside>
      </div>
    </>
  );
}

function QualityMetric({ label, value }: { label: string; value: number }) {
  return <div className="quality-metric"><span>{label}</span><strong>{value.toFixed(1)}%</strong><i><b style={{ width: `${value}%` }} /></i></div>;
}

export function RecipesPage({ workspace }: { workspace: WorkspaceData }) {
  const [selected, setSelected] = useState<RecipeOperator>(workspace.recipe.operators[0]);
  return (
    <>
      <PageHeader meta={`Updated ${formatDate(workspace.recipe.updated_at)}`} title={`Recipe v${workspace.recipe.version}`} description="A versioned sequence of small, inspectable operators.">
        <button className="secondary-button" type="button">Validate</button><button className="primary-button" type="button">Publish</button>
      </PageHeader>
      <div className="recipe-layout">
        <section className="panel operator-list">
          {workspace.recipe.operators.map((operator, index) => <button type="button" key={operator.id} data-selected={selected.id === operator.id} onClick={() => setSelected(operator)}><span className="operator-index">{index + 1}</span><span><strong>{operator.label}</strong><small>{operator.description}</small></span><Status state={operator.state} /><ArrowRightIcon /></button>)}
          <button className="add-operator" type="button"><PlusIcon />Add operator</button>
        </section>
        <aside className="panel operator-detail">
          <div className="panel-heading"><div><h2>{selected.label}</h2><p><code>{selected.operator}@{selected.version}</code></p></div><Status state={selected.state} /></div>
          <div className="detail-section"><span>Contract</span><div className="io-row"><code>record</code><em>object → object</em></div><div className="io-row"><code>decision</code><em>enum</em></div></div>
          <div className="detail-section"><span>Validation</span>{["Contract valid", "Required fields mapped", "Sample run passed"].map((item) => <div className="check-row" key={item}><CheckCircleIcon weight="fill" /><span>{item}</span><strong>Passed</strong></div>)}</div>
        </aside>
      </div>
    </>
  );
}

export function RunsPage({ workspace }: { workspace: WorkspaceData }) {
  const [selected, setSelected] = useState<RunSummary>(workspace.runs[0]);
  return (
    <>
      <PageHeader title="Runs" description="Execution history, throughput, and failure context."><button className="secondary-button" type="button"><ArrowDownIcon />Download logs</button></PageHeader>
      <div className="runs-layout">
        <section className="panel table-panel"><div className="plain-table-wrap"><table className="plain-table runs-table"><thead><tr><th>Run</th><th>Recipe</th><th>Started</th><th>Duration</th><th>Records</th><th>Result</th></tr></thead><tbody>{workspace.runs.map((run) => <tr key={run.id} data-selected={selected.id === run.id} onClick={() => setSelected(run)}><td><code>{run.id}</code></td><td>v{run.recipe_version}</td><td>{formatDate(run.started_at)}</td><td>{formatDuration(run.duration_seconds)}</td><td className="mono">{run.record_count.toLocaleString()}</td><td><Status state={run.state} /></td></tr>)}</tbody></table></div></section>
        <RunDetail run={selected} stages={workspace.stages} />
      </div>
    </>
  );
}

function RunDetail({ run, stages }: { run: RunSummary; stages: PipelineStage[] }) {
  const maxCount = Math.max(...stages.map((stage) => stage.count), 1);
  return (
    <aside className="panel run-detail">
      <div className="panel-heading"><div><h2>{run.id}</h2><p>{formatDate(run.started_at)}</p></div><Status state={run.state} /></div>
      <div className="run-stats"><div><strong>{run.record_count.toLocaleString()}</strong><span>Input</span></div><div><strong>{run.ready_count.toLocaleString()}</strong><span>Ready</span></div><div><strong>{run.review_count}</strong><span>Review</span></div><div><strong>{formatDuration(run.duration_seconds)}</strong><span>Duration</span></div></div>
      <div className="stage-timings"><span>Stage records</span>{stages.slice(0, 6).map((stage) => <div key={stage.id}><small>{stage.label}</small><i><b style={{ width: `${Math.max(4, 100 * stage.count / maxCount)}%` }} /></i><code>{stage.count.toLocaleString()}</code></div>)}</div>
      {run.failure_reason ? <p className="run-failure">{run.failure_reason}</p> : null}
      <button className="secondary-button full-button" type="button"><DownloadSimpleIcon />Download logs</button>
    </aside>
  );
}

export function ReviewPage({ records, total, onDecision }: { records: EvidenceRecord[]; total: number; onDecision: (id: string, decision: Decision) => void }) {
  const queue = records.filter((record) => record.decision === "review");
  const [selectedId, setSelectedId] = useState(queue[0]?.id ?? records[0]?.id);
  const selected = queue.find((record) => record.id === selectedId) ?? queue[0];
  return (
    <>
      <PageHeader title="Review" description={`${total} uncertain decisions need a person.`} />
      <div className="review-layout">
        <section className="panel review-queue">
          <div className="queue-header"><strong>Queue</strong><span>{total}</span></div>
          {queue.slice(0, 10).map((record) => <button className="queue-item" type="button" key={record.id} data-selected={selected?.id === record.id} onClick={() => setSelectedId(record.id)}><span><code>{record.id.replace("evt_", "rec_")}</code><small>{record.signal_type.replaceAll("_", " ")}</small></span><time>{Math.round(record.confidence * 100)}%</time><ArrowRightIcon /></button>)}
          <div className="queue-foot">{queue.length} sampled · {total} total</div>
        </section>
        {selected ? <ReviewInspector record={selected} onDecision={onDecision} /> : <section className="panel empty-state"><CheckCircleIcon size={30} weight="fill" /><strong>Queue cleared</strong><span>No records need review.</span></section>}
      </div>
    </>
  );
}

function ReviewInspector({ record, onDecision }: { record: EvidenceRecord; onDecision: (id: string, decision: Decision) => void }) {
  return (
    <section className="panel review-inspector">
      <div className="review-titlebar">
        <div><span>Decision review</span><h2>{record.id.replace("evt_", "rec_")}</h2><p>{formatDate(record.occurred_at)} · {record.batch_id}</p></div>
        <span className="confidence-tag">{Math.round(record.confidence * 100)}% confidence</span>
      </div>
      <div className="review-compare">
        <div><span>Source record</span><p className="source-excerpt">{record.raw_event}</p><FieldList fields={record.before_fields} /></div>
        <div className="proposed"><span>Proposed result</span><h3>{record.extracted_signal}</h3><FieldList fields={record.after_fields} /></div>
      </div>
      <div className="evidence-summary">
        <div><span>Quality</span><strong>{Math.round(record.quality_score * 100)}%</strong></div>
        <div><span>Matched evidence</span><strong>{record.evidence_count}</strong></div>
        <div><span>Privacy</span><strong>Approved</strong></div>
      </div>
      <div className="decision-reason"><span>Why this result</span><p>{record.reason}</p></div>
      <details className="evidence-disclosure"><summary><ShieldCheckIcon /><span>Evidence & lineage</span><small>Open only when needed</small><ArrowRightIcon /></summary><dl><div><dt>Privacy</dt><dd>{record.privacy}</dd></div><div><dt>Batch</dt><dd><code>{record.batch_id}</code></dd></div><div><dt>Metadata</dt><dd><code>{JSON.stringify(record.metadata)}</code></dd></div></dl></details>
      <div className="review-actions"><button className="accept-button" type="button" onClick={() => onDecision(record.id, "accepted")}><CheckCircleIcon />Accept</button><button className="secondary-button" type="button" onClick={() => onDecision(record.id, "modified")}><PencilSimpleIcon />Edit result</button><button className="reject-button" type="button" onClick={() => onDecision(record.id, "rejected")}><XCircleIcon />Reject</button></div>
    </section>
  );
}

function FieldList({ fields }: { fields: Record<string, string> }) {
  return <dl className="field-pairs">{Object.entries(fields).slice(0, 3).map(([key, value]) => <div key={key}><dt>{key}</dt><dd>{value}</dd></div>)}</dl>;
}

export function OutputsPage({ workspace, onDownload }: { workspace: WorkspaceData; onDownload: () => void }) {
  return (
    <>
      <PageHeader title="Outputs" description="Versioned snapshots ready for analytics and model workflows."><button className="primary-button" type="button" onClick={onDownload}><DownloadSimpleIcon />Export manifest</button></PageHeader>
      <section className="panel table-panel outputs-panel"><div className="panel-heading"><div><h2>Published artifacts</h2><p>Built from <code>{workspace.run_id}</code></p></div></div><div className="plain-table-wrap"><table className="plain-table"><thead><tr><th>Output</th><th>Format</th><th>Records</th><th>Created</th><th>Size</th><th>State</th><th /></tr></thead><tbody>{workspace.outputs.map((output) => <tr key={output.id}><td><span className="file-cell"><RowsIcon /><span><strong>{output.name}</strong><small>{output.id}</small></span></span></td><td><code>{output.format}</code></td><td className="mono">{output.record_count.toLocaleString()}</td><td>{formatDate(output.created_at)}</td><td>{output.size}</td><td><Status state={output.state} /></td><td><button className="icon-button" type="button" aria-label={`Download ${output.name}`} onClick={onDownload}><DownloadSimpleIcon /></button></td></tr>)}</tbody></table></div></section>
      <section className="publish-boundary"><LockKeyIcon /><div><strong>Publication boundary</strong><span>{workspace.copy_policy.cloud}</span></div><button className="text-link" type="button">View policy <ArrowRightIcon /></button></section>
    </>
  );
}

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("en", { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }).format(date);
}

function formatDuration(seconds: number) {
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`;
}
