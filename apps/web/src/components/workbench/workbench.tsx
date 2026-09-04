"use client";

import { useState, useTransition } from "react";
import {
  ArchiveTrayIcon,
  CaretDownIcon,
  CheckCircleIcon,
  DatabaseIcon,
  ExportIcon,
  FileTextIcon,
  LockKeyIcon,
  PlayIcon,
  StackIcon,
} from "@phosphor-icons/react";
import { DropdownMenu } from "@radix-ui/themes";
import type { BatchSummary, Decision, EvidenceRecord, PageName, WorkspaceData } from "@/lib/contracts";
import { AddDataDialog, type DialogName, RecordDialog, StepDialog } from "./dialogs";
import { DataPage, OutputsPage, PipelinePage, RecipesPage, ReviewPage, RunsPage } from "./pages";

const navItems = [
  { id: "pipeline", label: "Pipeline", icon: StackIcon },
  { id: "data", label: "Data", icon: DatabaseIcon },
  { id: "recipes", label: "Recipes", icon: FileTextIcon },
  { id: "runs", label: "Runs", icon: PlayIcon },
  { id: "review", label: "Review", icon: ArchiveTrayIcon },
  { id: "outputs", label: "Outputs", icon: ExportIcon },
] satisfies Array<{ id: PageName; label: string; icon: typeof StackIcon }>;

function downloadManifest(workspace: WorkspaceData) {
  const payload = JSON.stringify({ dataset: workspace.dataset, run_id: workspace.run_id, generated_at: workspace.generated_at, stages: workspace.stages, outputs: workspace.outputs, contract: "verity.snapshot.v2" }, null, 2);
  const url = URL.createObjectURL(new Blob([payload], { type: "application/json" }));
  const anchor = document.createElement("a"); anchor.href = url; anchor.download = `${workspace.run_id}-manifest.json`; anchor.click(); URL.revokeObjectURL(url);
}

export function Workbench({ initialWorkspace }: { initialWorkspace: WorkspaceData }) {
  const [workspace, setWorkspace] = useState(initialWorkspace);
  const [page, setPage] = useState<PageName>("pipeline");
  const [selectedStageId, setSelectedStageId] = useState("signals");
  const [dialog, setDialog] = useState<DialogName>(null);
  const [record, setRecord] = useState<EvidenceRecord | null>(null);
  const [lastRun, setLastRun] = useState("8m ago");
  const [isRunning, startRun] = useTransition();
  const stage = workspace.stages.find((item) => item.id === selectedStageId) ?? workspace.stages[4];

  function changeDecision(recordId: string, decision: Decision) {
    setWorkspace((current) => {
      const previous = current.records.find((item) => item.id === recordId)?.decision;
      const breakdown = { ...current.decision_breakdown };
      if (previous === "review") breakdown.review = Math.max(0, breakdown.review - 1);
      if (previous === "accepted" || previous === "modified") breakdown.accepted = Math.max(0, breakdown.accepted - 1);
      if (previous === "rejected") breakdown.rejected = Math.max(0, breakdown.rejected - 1);
      if (decision === "review") breakdown.review += 1;
      if (decision === "accepted" || decision === "modified") breakdown.accepted += 1;
      if (decision === "rejected") breakdown.rejected += 1;
      return { ...current, decision_breakdown: breakdown, records: current.records.map((item) => item.id === recordId ? { ...item, decision } : item), stages: current.stages.map((item) => item.id === "review" ? { ...item, count: breakdown.review } : item.id === "curated" ? { ...item, count: breakdown.accepted } : item) };
    });
    const apiUrl = process.env.NEXT_PUBLIC_API_URL;
    if (apiUrl && decision !== "review") void fetch(`${apiUrl}/api/v1/reviews/${recordId}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ decision, note: "" }) }).catch(() => undefined);
  }

  function runPipeline() {
    startRun(async () => {
      const apiUrl = process.env.NEXT_PUBLIC_API_URL;
      if (apiUrl) await fetch(`${apiUrl}/api/v1/runs`, { method: "POST" }).catch(() => undefined);
      await new Promise((resolve) => setTimeout(resolve, 650)); setLastRun("just now");
    });
  }

  function stageBatch(batch: BatchSummary) {
    setWorkspace((current) => ({ ...current, batches: [batch, ...current.batches], dataset: { ...current.dataset, batch_count: current.dataset.batch_count + 1, record_count: current.dataset.record_count + batch.record_count, updated_at: batch.added_at } }));
  }

  function openRecord(next: EvidenceRecord) { setRecord(next); setDialog("record"); }

  return <main className="app-shell">
    <header className="topbar">
      <div className="brand-lockup"><strong>VERITY</strong></div>
      <div className="breadcrumb"><span>Datasets</span><b>/</b><strong>{workspace.dataset.name}</strong></div>
      <div className="topbar-actions">
        <DropdownMenu.Root><DropdownMenu.Trigger><button className="top-control" type="button"><DatabaseIcon size={16} /> {workspace.dataset.name} <CaretDownIcon size={13} /></button></DropdownMenu.Trigger><DropdownMenu.Content align="end"><DropdownMenu.Item>{workspace.dataset.name}</DropdownMenu.Item><DropdownMenu.Separator /><DropdownMenu.Item>Create dataset</DropdownMenu.Item></DropdownMenu.Content></DropdownMenu.Root>
        <button className="workspace-state" type="button"><i /><span>Local workspace</span><CaretDownIcon /></button><button className="avatar" type="button" aria-label="Profile">AK</button>
      </div>
    </header>

    <aside className="left-rail"><nav aria-label="Workspace navigation">{navItems.map((item) => { const Icon = item.icon; const count = item.id === "review" ? workspace.decision_breakdown.review : null; return <button key={item.id} type="button" data-active={page === item.id} onClick={() => setPage(item.id)}><Icon size={19} /><span>{item.label}</span>{count ? <small>{count}</small> : null}</button>; })}</nav><div className="rail-spacer" /><button className="privacy-link" type="button"><LockKeyIcon /><span>Local by default</span><CheckCircleIcon weight="fill" /></button></aside>

    <section className="workspace-canvas">
      {page === "pipeline" ? <PipelinePage workspace={workspace} selectedStageId={selectedStageId} onStage={setSelectedStageId} onRun={runPipeline} isRunning={isRunning} onStep={() => setDialog("step")} onRecord={openRecord} onDecision={changeDecision} lastRun={lastRun} /> : null}
      {page === "data" ? <DataPage workspace={workspace} onAdd={() => setDialog("add-data")} /> : null}
      {page === "recipes" ? <RecipesPage workspace={workspace} /> : null}
      {page === "runs" ? <RunsPage workspace={workspace} /> : null}
      {page === "review" ? <ReviewPage records={workspace.records} total={workspace.decision_breakdown.review} onDecision={changeDecision} /> : null}
      {page === "outputs" ? <OutputsPage workspace={workspace} onDownload={() => downloadManifest(workspace)} /> : null}
    </section>

    <RecordDialog record={record} open={dialog === "record"} onOpenChange={(open) => setDialog(open ? "record" : null)} />
    <StepDialog workspace={workspace} stage={stage} open={dialog === "step"} onOpenChange={(open) => setDialog(open ? "step" : null)} />
    <AddDataDialog datasetId={workspace.dataset.id} open={dialog === "add-data"} onOpenChange={(open) => setDialog(open ? "add-data" : null)} onStaged={stageBatch} />
  </main>;
}
