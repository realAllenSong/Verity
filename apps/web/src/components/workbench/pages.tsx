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

function PageHeader({ eyebrow, title, description, children }: { eyebrow?: string; title: string; description?: string; children?: React.ReactNode }) {
  return <header className="page-header"><div>{eyebrow ? <span className="eyebrow">{eyebrow}</span> : null}<h1>{title}</h1>{description ? <p>{description}</p> : null}</div>{children ? <div className="page-actions">{children}</div> : null}</header>;
}

function Status({ state }: { state: string }) {
  const normalized = ["succeeded", "complete", "configured", "published", "ready"].includes(state) ? "success" : state === "failed" ? "failed" : state === "warning" || state === "review" ? "warning" : "neutral";
  return <span className="status-label" data-state={normalized}>{normalized === "success" ? <CheckCircleIcon weight="fill" /> : normalized === "failed" ? <XCircleIcon weight="fill" /> : normalized === "warning" ? <WarningCircleIcon weight="fill" /> : <CircleIcon weight="fill" />}{state}</span>;
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
  return <>
    <PageHeader eyebrow={`Recipe v${workspace.recipe.version} · Last run ${lastRun}`} title={workspace.dataset.name} description="Prepare and inspect every decision from raw record to ready output.">
      <button className="secondary-button" type="button" onClick={onStep}><FunnelIcon size={17} /> Configure</button>
      <button className="primary-button" type="button" onClick={onRun} disabled={isRunning}><PlayIcon weight="fill" size={16} /> {isRunning ? "Running…" : "Run pipeline"}</button>
    </PageHeader>
    <PipelineFlow stages={workspace.stages} selectedStage={selectedStageId} onSelect={onStage} />
    <section className="panel stage-panel">
      <div className="panel-heading"><div><h2>{selected.label}</h2><p>{selected.description}</p></div><button className="quiet-button" type="button" onClick={onStep}><InfoIcon /> Step details</button></div>
      <div className="tabbar" role="tablist">{(["records", "distribution", "schema"] as TabName[]).map((name) => <button key={name} role="tab" aria-selected={tab === name} onClick={() => setTab(name)}>{name[0].toUpperCase() + name.slice(1)}</button>)}<span>{selected.count.toLocaleString()} records</span></div>
      {tab === "records" ? <><RecordTable records={visible.slice(0, 7)} selectedRecordId={null} onOpenRecord={onRecord} onDecision={onDecision} /><TablePager shown={Math.min(visible.length, 7)} total={selected.count} /></> : null}
      {tab === "distribution" ? <DistributionView data={workspace.signal_distribution} /> : null}
      {tab === "schema" ? <SchemaView workspace={workspace} /> : null}
    </section>
  </>;
}

function TablePager({ shown, total }: { shown: number; total: number }) {
  return <footer className="table-footer"><span>{shown} of {total.toLocaleString()}</span><div><button disabled type="button">Previous</button><strong>1</strong><button type="button">Next</button></div></footer>;
}

export function DataPage({ workspace, onAdd }: { workspace: WorkspaceData; onAdd: () => void }) {
  const { dataset } = workspace;
  return <>
    <PageHeader title="Data" description="Profile the dataset and add independent batches over time."><button className="primary-button" type="button" onClick={onAdd}><PlusIcon size={17} /> Add data</button></PageHeader>
    <section className="panel dataset-profile">
      <div className="metric-strip"><div><strong>{dataset.record_count.toLocaleString()}</strong><span>records</span></div><div><strong>{dataset.field_count}</strong><span>fields</span></div><div><strong>{dataset.batch_count}</strong><span>batches</span></div><div><strong>8m</strong><span>last updated</span></div></div>
      <div className="quality-strip"><QualityMetric label="Completeness" value={dataset.completeness} /><QualityMetric label="Validity" value={dataset.validity} /></div>
    </section>
    <div className="data-layout">
      <section className="panel table-panel"><div className="panel-heading"><div><h2>Ingestion batches</h2><p>Each batch remains independently traceable.</p></div><button className="icon-button" aria-label="Batch options"><DotsThreeIcon /></button></div>
        <div className="plain-table-wrap"><table className="plain-table"><thead><tr><th>Batch</th><th>Added</th><th>Records</th><th>Fields</th><th>State</th></tr></thead><tbody>{workspace.batches.map((batch) => <tr key={batch.id}><td><span className="file-cell"><FileIcon size={17} /><span><strong>{batch.filename}</strong><small>{batch.id}</small></span></span></td><td>{formatDate(batch.added_at)}</td><td className="mono">{batch.record_count.toLocaleString()}</td><td className="mono">{batch.field_count}</td><td><Status state={batch.state} /></td></tr>)}</tbody></table></div>
      </section>
      <aside className="panel schema-rail"><div className="panel-heading"><div><h2>Schema</h2><p>{dataset.field_count} inferred fields</p></div><CodeIcon /></div>{[...workspace.schema_before, ...workspace.schema_after].slice(0, 7).map((field) => <div className="schema-line" key={field.field}><code>{field.field}</code><span>{field.type}</span></div>)}<button className="text-link" type="button">View all fields <ArrowRightIcon /></button></aside>
    </div>
  </>;
}

function QualityMetric({ label, value }: { label: string; value: number }) {
  return <div className="quality-metric"><span>{label}</span><strong>{value.toFixed(1)}%</strong><i><b style={{ width: `${value}%` }} /></i></div>;
}

export function RecipesPage({ workspace }: { workspace: WorkspaceData }) {
  const [selected, setSelected] = useState<RecipeOperator>(workspace.recipe.operators[0]);
  return <>
    <PageHeader eyebrow={`Updated ${formatDate(workspace.recipe.updated_at)}`} title={`Recipe v${workspace.recipe.version}`} description="A readable, versioned sequence of operators.">
      <button className="secondary-button" type="button">Validate</button><button className="primary-button" type="button">Publish</button>
    </PageHeader>
    <div className="recipe-layout"><section className="panel operator-list">{workspace.recipe.operators.map((operator, index) => <button type="button" key={operator.id} data-selected={selected.id === operator.id} onClick={() => setSelected(operator)}><span className="operator-index">{index + 1}</span><span><strong>{operator.label}</strong><small>{operator.description}</small></span><Status state={operator.state} /><ArrowRightIcon /></button>)}<button className="add-operator" type="button"><PlusIcon /> Add operator</button></section>
      <aside className="panel operator-detail"><div className="panel-heading"><div><h2>{selected.label}</h2><p><code>{selected.operator}@{selected.version}</code></p></div><Status state={selected.state} /></div><div className="detail-section"><span>Input</span><div className="io-row"><code>record</code><em>object</em></div></div><div className="detail-section"><span>Outputs</span><div className="io-row"><code>record</code><em>object</em></div><div className="io-row"><code>decision</code><em>enum</em></div></div><div className="detail-section"><span>Validation</span>{["Contract is valid", "Required fields mapped", "Sample run passed"].map((item) => <div className="check-row" key={item}><CheckCircleIcon weight="fill" /><span>{item}</span><strong>100%</strong></div>)}</div></aside>
    </div>
  </>;
}

export function RunsPage({ workspace }: { workspace: WorkspaceData }) {
  const [selected, setSelected] = useState<RunSummary>(workspace.runs[0]);
  return <>
    <PageHeader title="Runs" description="Compare execution history, throughput, and stage health."><button className="secondary-button" type="button"><ArrowDownIcon /> Download logs</button></PageHeader>
    <div className="runs-layout"><section className="panel table-panel"><div className="plain-table-wrap"><table className="plain-table runs-table"><thead><tr><th>Run</th><th>Recipe</th><th>Started</th><th>Duration</th><th>Records</th><th>Result</th></tr></thead><tbody>{workspace.runs.map((run) => <tr key={run.id} data-selected={selected.id === run.id} onClick={() => setSelected(run)}><td><code>{run.id}</code></td><td>v{run.recipe_version}</td><td>{formatDate(run.started_at)}</td><td>{formatDuration(run.duration_seconds)}</td><td className="mono">{run.record_count.toLocaleString()}</td><td><Status state={run.state} /></td></tr>)}</tbody></table></div></section>
      <RunDetail run={selected} stages={workspace.stages} />
    </div>
  </>;
}

function RunDetail({ run, stages }: { run: RunSummary; stages: PipelineStage[] }) {
  return <aside className="panel run-detail"><div className="panel-heading"><div><h2>{run.id}</h2><p>{formatDate(run.started_at)}</p></div><Status state={run.state} /></div><div className="run-stats"><div><strong>{run.record_count.toLocaleString()}</strong><span>input</span></div><div><strong>{run.ready_count.toLocaleString()}</strong><span>ready</span></div><div><strong>{run.review_count}</strong><span>review</span></div><div><strong>{formatDuration(run.duration_seconds)}</strong><span>duration</span></div></div><div className="stage-timings"><span>Stage timing</span>{stages.slice(0, 6).map((stage, index) => <div key={stage.id}><small>{stage.label}</small><i><b style={{ width: `${32 + index * 9}%` }} /></i><code>{18 + index * 4}s</code></div>)}</div><button className="secondary-button full-button" type="button"><DownloadSimpleIcon /> Download logs</button></aside>;
}

export function ReviewPage({ records, total, onDecision }: { records: EvidenceRecord[]; total: number; onDecision: (id: string, decision: Decision) => void }) {
  const queue = records.filter((record) => record.decision === "review");
  const [selectedId, setSelectedId] = useState(queue[0]?.id ?? records[0]?.id);
  const selected = records.find((record) => record.id === selectedId) ?? queue[0] ?? records[0];
  return <>
    <PageHeader title="Review" description={`${total} uncertain decisions need a person.`} />
    <div className="review-layout"><section className="panel review-queue"><div className="queue-tabs"><button data-active="true">Queue <span>{total}</span></button><button>Resolved</button></div>{queue.slice(0, 10).map((record) => <button className="queue-item" type="button" key={record.id} data-selected={selected?.id === record.id} onClick={() => setSelectedId(record.id)}><span><code>{record.id.replace("evt_", "rec_")}</code><small>{record.signal_type.replaceAll("_", " ")}</small></span><time>{record.confidence.toFixed(2)}</time><ArrowRightIcon /></button>)}<div className="queue-foot">Showing {queue.length} sampled records of {total}</div></section>
      {selected ? <section className="panel review-inspector"><div className="panel-heading"><div><h2>{selected.id.replace("evt_", "rec_")}</h2><p>{formatDate(selected.occurred_at)}</p></div><span className="confidence-tag">{selected.confidence.toFixed(2)} confidence</span></div><div className="review-compare"><div><span>Before</span><pre>{JSON.stringify(selected.before_fields, null, 2)}</pre></div><div className="proposed"><span>Proposed</span><pre>{JSON.stringify(selected.after_fields, null, 2)}</pre></div></div><div className="decision-reason"><span>Reason</span><p>{selected.reason}</p></div><div className="review-actions"><button className="accept-button" type="button" onClick={() => onDecision(selected.id, "accepted")}><CheckCircleIcon /> Accept</button><button className="secondary-button" type="button" onClick={() => onDecision(selected.id, "modified")}><PencilSimpleIcon /> Modify</button><button className="reject-button" type="button" onClick={() => onDecision(selected.id, "rejected")}><XCircleIcon /> Reject</button></div><button className="evidence-row" type="button"><ShieldCheckIcon /><span>Decision evidence</span><small>{selected.evidence_count} matched records</small><ArrowRightIcon /></button></section> : <section className="panel empty-state"><CheckCircleIcon size={28} /><strong>Queue cleared</strong><span>No records need review.</span></section>}
    </div>
  </>;
}

export function OutputsPage({ workspace, onDownload }: { workspace: WorkspaceData; onDownload: () => void }) {
  return <>
    <PageHeader title="Outputs" description="Versioned snapshots ready for analytics and model workflows."><button className="primary-button" type="button" onClick={onDownload}><DownloadSimpleIcon /> Export manifest</button></PageHeader>
    <section className="panel table-panel outputs-panel"><div className="panel-heading"><div><h2>Published artifacts</h2><p>Built from <code>{workspace.run_id}</code></p></div></div><div className="plain-table-wrap"><table className="plain-table"><thead><tr><th>Output</th><th>Format</th><th>Records</th><th>Created</th><th>Size</th><th>State</th><th /></tr></thead><tbody>{workspace.outputs.map((output) => <tr key={output.id}><td><span className="file-cell"><RowsIcon /><span><strong>{output.name}</strong><small>{output.id}</small></span></span></td><td><code>{output.format}</code></td><td className="mono">{output.record_count.toLocaleString()}</td><td>{formatDate(output.created_at)}</td><td>{output.size}</td><td><Status state={output.state} /></td><td><button className="icon-button" type="button" aria-label={`Download ${output.name}`} onClick={onDownload}><DownloadSimpleIcon /></button></td></tr>)}</tbody></table></div></section>
    <section className="publish-boundary"><LockKeyIcon /><div><strong>Publication boundary</strong><span>{workspace.copy_policy.cloud}</span></div><button className="text-link" type="button">View policy <ArrowRightIcon /></button></section>
  </>;
}

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("en", { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }).format(date);
}

function formatDuration(seconds: number) {
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`;
}
