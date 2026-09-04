"use client";

import { useMemo, useState, useTransition } from "react";
import {
  ArchiveTrayIcon,
  CaretDownIcon,
  CheckCircleIcon,
  ClockCounterClockwiseIcon,
  DatabaseIcon,
  ExportIcon,
  FileTextIcon,
  InfoIcon,
  LockKeyIcon,
  PlayIcon,
  SlidersHorizontalIcon,
  StackIcon,
} from "@phosphor-icons/react";
import { DropdownMenu } from "@radix-ui/themes";
import type { Decision, EvidenceRecord, WorkspaceData } from "@/lib/contracts";
import { WorkspaceDialogs, type DialogName } from "./dialogs";
import { PipelineFlow } from "./pipeline-flow";
import { RecordTable } from "./record-table";
import { DistributionView, SchemaView } from "./stage-views";

type TabName = "records" | "distribution" | "schema";

const navItems = [
  { id: "pipeline", label: "Pipeline", icon: StackIcon },
  { id: "sources", label: "Sources", icon: DatabaseIcon },
  { id: "recipe", label: "Recipes", icon: FileTextIcon },
  { id: "runs", label: "Runs", icon: PlayIcon },
  { id: "review", label: "Review", icon: ArchiveTrayIcon },
  { id: "export", label: "Exports", icon: ExportIcon },
] as const;

const dialogForNav: Partial<Record<(typeof navItems)[number]["id"], DialogName>> = {
  sources: "sources",
  recipe: "recipe",
  runs: "step",
  review: "review",
  export: "export",
};

function downloadManifest(workspace: WorkspaceData) {
  const payload = JSON.stringify(
    {
      run_id: workspace.run_id,
      generated_at: workspace.generated_at,
      stages: workspace.stages,
      decision_breakdown: workspace.decision_breakdown,
      contract: "verity.snapshot.v1",
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

export function Workbench({ initialWorkspace }: { initialWorkspace: WorkspaceData }) {
  const [workspace, setWorkspace] = useState(initialWorkspace);
  const [selectedStageId, setSelectedStageId] = useState("signals");
  const [activeTab, setActiveTab] = useState<TabName>("records");
  const [activeDialog, setActiveDialog] = useState<DialogName>(null);
  const [selectedRecord, setSelectedRecord] = useState<EvidenceRecord | null>(workspace.records[0] ?? null);
  const [lastRun, setLastRun] = useState("28m ago");
  const [isRunning, startRun] = useTransition();

  const selectedStage = workspace.stages.find((stage) => stage.id === selectedStageId) ?? workspace.stages[4];
  const visibleRecords = useMemo(() => {
    const records = selectedStageId === "review"
      ? workspace.records.filter((record) => record.decision === "review")
      : selectedStageId === "curated"
        ? workspace.records.filter((record) => record.decision !== "review" && record.decision !== "rejected")
        : workspace.records;
    return records.slice(0, 6);
  }, [selectedStageId, workspace.records]);

  const readyCount = workspace.stages.find((stage) => stage.id === "curated")?.count ?? 0;
  const reviewCount = workspace.decision_breakdown.review;
  const readyRate = workspace.stages[4]?.count
    ? (workspace.decision_breakdown.accepted / workspace.stages[4].count) * 100
    : 0;

  function changeDecision(recordId: string, decision: Decision) {
    setWorkspace((current) => {
      const previous = current.records.find((record) => record.id === recordId)?.decision;
      const breakdown = { ...current.decision_breakdown };
      if (previous === "review") breakdown.review = Math.max(0, breakdown.review - 1);
      if (previous === "accepted") breakdown.accepted = Math.max(0, breakdown.accepted - 1);
      if (previous === "rejected") breakdown.rejected = Math.max(0, breakdown.rejected - 1);
      if (decision === "review") breakdown.review += 1;
      if (decision === "accepted" || decision === "modified") breakdown.accepted += 1;
      if (decision === "rejected") breakdown.rejected += 1;
      return {
        ...current,
        decision_breakdown: breakdown,
        records: current.records.map((record) => record.id === recordId ? { ...record, decision } : record),
      };
    });

    const apiUrl = process.env.NEXT_PUBLIC_API_URL;
    if (apiUrl && decision !== "review") {
      void fetch(`${apiUrl}/api/v1/reviews/${recordId}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ decision, note: "" }),
      }).catch(() => undefined);
    }
  }

  function runRecipe() {
    startRun(async () => {
      const apiUrl = process.env.NEXT_PUBLIC_API_URL;
      if (apiUrl) {
        await fetch(`${apiUrl}/api/v1/runs`, { method: "POST" }).catch(() => undefined);
      }
      await new Promise((resolve) => setTimeout(resolve, 700));
      setLastRun("just now");
    });
  }

  function openRecord(record: EvidenceRecord) {
    setSelectedRecord(record);
    setActiveDialog("record");
  }

  return (
    <main className="app-shell">
      <header className="topbar">
        <div className="brand-lockup"><strong>VERITY</strong><span>Data preparation</span></div>
        <div className="breadcrumb"><span>Synthetic workspace</span><b>/</b><strong>Workflow signals</strong></div>
        <div className="topbar-actions">
          <DropdownMenu.Root>
            <DropdownMenu.Trigger><button className="top-control" type="button">Sample data <CaretDownIcon size={14} /></button></DropdownMenu.Trigger>
            <DropdownMenu.Content align="end"><DropdownMenu.Item>Sample data</DropdownMenu.Item><DropdownMenu.Item disabled>Connect workspace</DropdownMenu.Item></DropdownMenu.Content>
          </DropdownMenu.Root>
          <button className="top-control date-control" type="button"><ClockCounterClockwiseIcon size={17} /> Last 30 days <CaretDownIcon size={14} /></button>
          <button className="avatar" type="button" aria-label="Open profile">MK</button>
        </div>
      </header>

      <aside className="left-rail">
        <button className="project-switcher" type="button">Workflow signals <CaretDownIcon size={15} /></button>
        <div className="health-line">
          <span><CheckCircleIcon size={17} weight="fill" /> {readyCount.toLocaleString()} ready</span>
          <span>{reviewCount.toLocaleString()} to review</span>
        </div>
        <nav aria-label="Workspace navigation">
          {navItems.map((item) => {
            const Icon = item.icon;
            const count = item.id === "sources" ? workspace.sources.length : item.id === "review" ? reviewCount : null;
            return (
              <button
                key={item.id}
                type="button"
                data-active={item.id === "pipeline"}
                onClick={() => {
                  if (item.id === "pipeline") return;
                  setActiveDialog(dialogForNav[item.id] ?? null);
                }}
              >
                <Icon size={20} />
                <span>{item.label}</span>
                {count !== null ? <small>{count}</small> : null}
              </button>
            );
          })}
        </nav>
        <button className="privacy-link" type="button" onClick={() => setActiveDialog("recipe")}>
          <LockKeyIcon size={18} /> Local-first privacy <InfoIcon size={15} />
        </button>
      </aside>

      <section className="workspace-canvas">
        <div className="workspace-header">
          <div><h1>Workflow signals</h1><p>Prepare noisy events for downstream models.</p></div>
          <div className="workspace-actions">
            <button className="primary-button" type="button" onClick={runRecipe} disabled={isRunning}>
              <PlayIcon size={17} weight="fill" /> {isRunning ? "Running" : "Run recipe"}
            </button>
            <button className="secondary-button recipe-details-button" type="button" onClick={() => setActiveDialog("recipe")}>
              <InfoIcon size={18} /> Recipe details
            </button>
          </div>
        </div>

        <PipelineFlow stages={workspace.stages} selectedStage={selectedStageId} onSelect={setSelectedStageId} />

        <section className="stage-section">
          <div className="stage-heading">
            <h2>{selectedStage.label}</h2>
            <div className="stage-metrics">
              <strong>{selectedStage.count.toLocaleString()}</strong><span>records</span>
              <i />
              <strong className="ready-number">{readyRate.toFixed(1)}%</strong><span>ready</span>
              <i />
              <strong className="review-number">{reviewCount}</strong><span>uncertain</span>
            </div>
            <button className="secondary-button step-button" type="button" onClick={() => setActiveDialog("step")}>
              <SlidersHorizontalIcon size={18} /> Step details
            </button>
          </div>

          <div className="stage-tabs" role="tablist" aria-label="Stage views">
            {(["records", "distribution", "schema"] as TabName[]).map((tab) => (
              <button key={tab} type="button" role="tab" aria-selected={activeTab === tab} onClick={() => setActiveTab(tab)}>
                {tab[0].toUpperCase() + tab.slice(1)}
              </button>
            ))}
            <span className="last-run">Updated {lastRun}</span>
          </div>

          {activeTab === "records" ? (
            <>
              <RecordTable records={visibleRecords} selectedRecordId={selectedRecord?.id ?? null} onOpenRecord={openRecord} onDecision={changeDecision} />
              <div className="table-footer"><span>{visibleRecords.length} of {selectedStage.count.toLocaleString()}</span><div><button type="button" disabled>Previous</button><strong>1</strong><button type="button">Next</button></div></div>
            </>
          ) : null}
          {activeTab === "distribution" ? <DistributionView data={workspace.signal_distribution} /> : null}
          {activeTab === "schema" ? <SchemaView workspace={workspace} /> : null}
        </section>
      </section>

      <WorkspaceDialogs
        active={activeDialog}
        onChange={setActiveDialog}
        workspace={workspace}
        stage={selectedStage}
        record={selectedRecord}
        onAccept={(recordId) => changeDecision(recordId, "accepted")}
        onDownload={() => downloadManifest(workspace)}
      />
    </main>
  );
}
