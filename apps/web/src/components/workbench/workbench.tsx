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
  WarningCircleIcon,
} from "@phosphor-icons/react";
import { DropdownMenu } from "@radix-ui/themes";
import type { BatchSummary, Decision, PageName, WorkspaceData } from "@/lib/contracts";
import { AddDataDialog, type DialogName, StepDialog } from "./dialogs";
import { DataPage, OutputsPage, PipelinePage, RecipesPage, ReviewPage, RunsPage } from "./pages";

const navItems = [
  { id: "pipeline", label: "Pipeline", icon: StackIcon },
  { id: "data", label: "Data", icon: DatabaseIcon },
  { id: "recipes", label: "Recipes", icon: FileTextIcon },
  { id: "runs", label: "Runs", icon: PlayIcon },
  { id: "review", label: "Review", icon: ArchiveTrayIcon },
  { id: "outputs", label: "Outputs", icon: ExportIcon },
] satisfies Array<{ id: PageName; label: string; icon: typeof StackIcon }>;

type Notice = { tone: "success" | "error"; message: string } | null;

function downloadManifest(workspace: WorkspaceData) {
  const payload = JSON.stringify(
    {
      dataset: workspace.dataset,
      run_id: workspace.run_id,
      generated_at: workspace.generated_at,
      stages: workspace.stages,
      outputs: workspace.outputs,
      contract: "verity.snapshot.v2",
    },
    null,
    2,
  );
  const url = URL.createObjectURL(new Blob([payload], { type: "application/json" }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `${workspace.run_id}-manifest.json`;
  anchor.click();
  URL.revokeObjectURL(url);
}

function updateLocalDecision(workspace: WorkspaceData, recordId: string, decision: Decision) {
  const previous = workspace.records.find((item) => item.id === recordId)?.decision;
  const breakdown = { ...workspace.decision_breakdown };
  if (previous === "review") breakdown.review = Math.max(0, breakdown.review - 1);
  if (previous === "accepted" || previous === "modified") breakdown.accepted = Math.max(0, breakdown.accepted - 1);
  if (previous === "rejected") breakdown.rejected = Math.max(0, breakdown.rejected - 1);
  if (decision === "review") breakdown.review += 1;
  if (decision === "accepted" || decision === "modified") breakdown.accepted += 1;
  if (decision === "rejected") breakdown.rejected += 1;
  return {
    ...workspace,
    decision_breakdown: breakdown,
    records: workspace.records.map((item) => (item.id === recordId ? { ...item, decision } : item)),
  };
}

export function Workbench({ initialWorkspace }: { initialWorkspace: WorkspaceData }) {
  const [workspace, setWorkspace] = useState(initialWorkspace);
  const [page, setPage] = useState<PageName>("pipeline");
  const [selectedStageId, setSelectedStageId] = useState("signals");
  const [dialog, setDialog] = useState<DialogName>(null);
  const [lastRun, setLastRun] = useState(() => relativeTime(initialWorkspace.generated_at));
  const [notice, setNotice] = useState<Notice>(null);
  const [isRunning, startRun] = useTransition();
  const stage = workspace.stages.find((item) => item.id === selectedStageId) ?? workspace.stages[4];
  const apiUrl = process.env.NEXT_PUBLIC_API_URL;

  function changeDecision(recordId: string, decision: Decision) {
    const before = workspace;
    setWorkspace((current) => updateLocalDecision(current, recordId, decision));
    setNotice({ tone: "success", message: "Decision saved locally" });
    if (!apiUrl || decision === "review") return;
    void (async () => {
      try {
        const response = await fetch(`${apiUrl}/api/v1/reviews/${recordId}`, {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ decision, note: "" }),
        });
        if (!response.ok) throw new Error(`review update returned ${response.status}`);
        setWorkspace((await response.json()) as WorkspaceData);
        setNotice({ tone: "success", message: "Review decision persisted" });
      } catch {
        setWorkspace(before);
        setNotice({ tone: "error", message: "Could not save the review decision" });
      }
    })();
  }

  function runPipeline() {
    startRun(async () => {
      setNotice(null);
      try {
        if (apiUrl) {
          const runResponse = await fetch(`${apiUrl}/api/v1/runs`, { method: "POST" });
          if (!runResponse.ok) throw new Error(`run returned ${runResponse.status}`);
          const workspaceResponse = await fetch(`${apiUrl}/api/v1/workspace`, { cache: "no-store" });
          if (!workspaceResponse.ok) throw new Error("workspace refresh failed");
          setWorkspace((await workspaceResponse.json()) as WorkspaceData);
        } else {
          await new Promise((resolve) => setTimeout(resolve, 650));
        }
        setLastRun("just now");
        setNotice({ tone: "success", message: "Pipeline completed" });
      } catch {
        setNotice({ tone: "error", message: "Pipeline failed. No published output was changed." });
      }
    });
  }

  function stageBatch(batch: BatchSummary) {
    setWorkspace((current) => {
      const exists = current.batches.some((item) => item.id === batch.id);
      return {
        ...current,
        batches: [batch, ...current.batches.filter((item) => item.id !== batch.id)],
        dataset: {
          ...current.dataset,
          batch_count: exists ? current.dataset.batch_count : current.dataset.batch_count + 1,
          record_count: exists ? current.dataset.record_count : current.dataset.record_count + batch.record_count,
          updated_at: batch.added_at,
          state: "attention",
        },
      };
    });
    setNotice({ tone: "success", message: "Batch staged for the next run" });
  }

  return (
    <main className="app-shell">
      <header className="topbar">
        <div className="brand-lockup"><span className="brand-mark">V</span><strong>VERITY</strong></div>
        <div className="breadcrumb"><span>Datasets</span><b>/</b><strong>{workspace.dataset.name}</strong></div>
        <div className="topbar-actions">
          <DropdownMenu.Root>
            <DropdownMenu.Trigger>
              <button className="top-control" type="button"><DatabaseIcon size={16} /> {workspace.dataset.name} <CaretDownIcon size={13} /></button>
            </DropdownMenu.Trigger>
            <DropdownMenu.Content align="end"><DropdownMenu.Item>{workspace.dataset.name}</DropdownMenu.Item><DropdownMenu.Separator /><DropdownMenu.Item>Create dataset</DropdownMenu.Item></DropdownMenu.Content>
          </DropdownMenu.Root>
          <span className="workspace-state"><i />Local workspace</span>
          <button className="avatar" type="button" aria-label="Profile">AK</button>
        </div>
      </header>

      <aside className="left-rail">
        <nav aria-label="Workspace navigation">
          {navItems.map((item) => {
            const Icon = item.icon;
            const count = item.id === "review" ? workspace.decision_breakdown.review : null;
            return <button key={item.id} type="button" aria-label={item.label} data-active={page === item.id} onClick={() => setPage(item.id)}><Icon size={19} /><span>{item.label}</span>{count ? <small>{count}</small> : null}</button>;
          })}
        </nav>
        <div className="rail-spacer" />
        <div className="privacy-link"><LockKeyIcon /><span>Local by default</span><CheckCircleIcon weight="fill" /></div>
      </aside>

      <section className="workspace-canvas">
        {notice ? <div className="operation-notice" data-tone={notice.tone} role="status">{notice.tone === "success" ? <CheckCircleIcon weight="fill" /> : <WarningCircleIcon weight="fill" />}{notice.message}<button type="button" aria-label="Dismiss notification" onClick={() => setNotice(null)}>×</button></div> : null}
        {page === "pipeline" ? <PipelinePage workspace={workspace} selectedStageId={selectedStageId} onStage={setSelectedStageId} onRun={runPipeline} isRunning={isRunning} onStep={() => setDialog("step")} lastRun={lastRun} /> : null}
        {page === "data" ? <DataPage workspace={workspace} onAdd={() => setDialog("add-data")} /> : null}
        {page === "recipes" ? <RecipesPage workspace={workspace} /> : null}
        {page === "runs" ? <RunsPage workspace={workspace} /> : null}
        {page === "review" ? <ReviewPage records={workspace.records} total={workspace.decision_breakdown.review} onDecision={changeDecision} /> : null}
        {page === "outputs" ? <OutputsPage workspace={workspace} onDownload={() => downloadManifest(workspace)} /> : null}
      </section>

      <StepDialog workspace={workspace} stage={stage} open={dialog === "step"} onOpenChange={(open) => setDialog(open ? "step" : null)} />
      <AddDataDialog datasetId={workspace.dataset.id} open={dialog === "add-data"} onOpenChange={(open) => setDialog(open ? "add-data" : null)} onStaged={stageBatch} />
    </main>
  );
}

function relativeTime(value: string) {
  const timestamp = new Date(value).getTime();
  if (!Number.isFinite(timestamp)) return "unknown";
  const elapsedMinutes = Math.max(0, Math.round((Date.now() - timestamp) / 60_000));
  if (elapsedMinutes < 1) return "just now";
  if (elapsedMinutes < 60) return `${elapsedMinutes}m ago`;
  const hours = Math.round(elapsedMinutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.round(hours / 24)}d ago`;
}
