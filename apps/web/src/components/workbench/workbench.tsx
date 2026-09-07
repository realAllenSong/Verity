"use client";

import { useState } from "react";
import {
  CaretDownIcon,
  CheckCircleIcon,
  ClockCounterClockwiseIcon,
  DotsThreeIcon,
  GearSixIcon,
  PlugsConnectedIcon,
  WarningCircleIcon,
} from "@phosphor-icons/react";
import { DropdownMenu } from "@radix-ui/themes";
import type { Decision, ImportSummary, JobSummary, WorkspaceData } from "@/lib/contracts";
import { ImportDropzone } from "./import-dropzone";
import { JobProgress } from "./job-progress";
import type { ActiveImport } from "./workspace-machine";
import { screenState } from "./workspace-machine";
import { PrivacyNote, ResultAction, ReviewDialog, WorkspaceView } from "./workspace-view";

const chunkBytes = 8 * 1024 * 1024;

type Notice = { tone: "success" | "error"; message: string } | null;

function updateLocalDecision(workspace: WorkspaceData, recordId: string, decision: Decision) {
  const previous = workspace.records.find((item) => item.id === recordId)?.decision;
  const breakdown = { ...workspace.decision_breakdown };
  if (previous === "review") breakdown.review = Math.max(0, breakdown.review - 1);
  if (previous === "accepted" || previous === "modified") breakdown.accepted = Math.max(0, breakdown.accepted - 1);
  if (previous === "rejected") breakdown.rejected = Math.max(0, breakdown.rejected - 1);
  if (decision === "accepted" || decision === "modified") breakdown.accepted += 1;
  if (decision === "rejected") breakdown.rejected += 1;
  return {
    ...workspace,
    decision_breakdown: breakdown,
    records: workspace.records.map((item) => item.id === recordId ? { ...item, decision } : item),
  };
}

export function Workbench({ initialWorkspace }: { initialWorkspace: WorkspaceData }) {
  const [workspace, setWorkspace] = useState(initialWorkspace);
  const [selectedStageId, setSelectedStageId] = useState("raw");
  const [activeImport, setActiveImport] = useState<ActiveImport>();
  const [reviewOpen, setReviewOpen] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);
  const apiUrl = process.env.NEXT_PUBLIC_API_URL;
  const busy = activeImport?.state === "uploading" || activeImport?.state === "processing";

  async function refreshWorkspace() {
    if (!apiUrl) return;
    const response = await fetch(`${apiUrl}/api/v1/workspace`, { cache: "no-store" });
    if (!response.ok) throw new Error("The workspace could not be refreshed.");
    setWorkspace(await response.json() as WorkspaceData);
  }

  async function waitForJob(job: JobSummary, current: ActiveImport) {
    if (!apiUrl) return;
    const deadline = Date.now() + 120_000;
    let latest = job;
    while (Date.now() < deadline) {
      const response = await fetch(`${apiUrl}${job.status_url}`, { cache: "no-store" });
      if (!response.ok) throw new Error("Processing status is unavailable.");
      latest = await response.json() as JobSummary;
      const state = screenState(latest);
      setActiveImport({ ...current, uploaded: current.bytes, state, job: latest, error: latest.error });
      if (latest.state === "succeeded") {
        await refreshWorkspace();
        setSelectedStageId("raw");
        setNotice({ tone: "success", message: "Result ready. Every stage can now be inspected." });
        return;
      }
      if (latest.state === "failed" || latest.state === "canceled") {
        throw new Error(latest.error || "Processing failed. The previous result is unchanged.");
      }
      await new Promise((resolve) => setTimeout(resolve, 350));
    }
    throw new Error("Processing is still running. You can safely return to this workspace later.");
  }

  async function uploadFile(file: File) {
    if (!apiUrl) throw new Error("Start the local API before adding data.");
    const current: ActiveImport = { filename: file.name, bytes: file.size, uploaded: 0, state: "uploading" };
    setActiveImport(current);
    setNotice(null);
    const idempotency = `${file.name}:${file.size}:${file.lastModified}`;
    const create = await fetch(`${apiUrl}/api/v1/imports`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "Idempotency-Key": idempotency },
      body: JSON.stringify({ dataset_id: workspace.dataset.id, filename: file.name, media_type: file.type, size_bytes: file.size }),
    });
    if (!create.ok) throw new Error(await problemDetail(create, "This file could not be accepted."));
    const imported = await create.json() as ImportSummary;
    let offset = imported.offset;
    if (offset > 0 && offset < file.size) {
      const head = await fetch(`${apiUrl}${imported.upload_url}`, { method: "HEAD" });
      if (!head.ok) throw new Error("The resumable upload could not be inspected.");
      offset = Number(head.headers.get("Upload-Offset") ?? offset);
    }
    while (offset < file.size) {
      const end = Math.min(file.size, offset + chunkBytes);
      const response = await fetch(`${apiUrl}${imported.upload_url}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/offset+octet-stream", "Upload-Offset": String(offset) },
        body: file.slice(offset, end),
      });
      if (!response.ok) throw new Error(await problemDetail(response, "Upload stopped before the chunk was committed."));
      offset = Number(response.headers.get("Upload-Offset") ?? end);
      setActiveImport({ ...current, uploaded: offset });
    }
    setActiveImport({ ...current, uploaded: file.size, state: "processing" });
    const complete = await fetch(`${apiUrl}/api/v1/imports/${imported.import_id}/complete`, {
      method: "POST",
      headers: { "Idempotency-Key": `complete:${idempotency}` },
    });
    if (!complete.ok) throw new Error(await problemDetail(complete, "The upload could not start processing."));
    const job = await complete.json() as JobSummary;
    await waitForJob(job, { ...current, uploaded: file.size, state: "processing", job });
  }

  async function addFiles(files: File[]) {
    for (const file of files) {
      try {
        await uploadFile(file);
      } catch (failure) {
        const message = failure instanceof Error ? failure.message : "Import failed.";
        setActiveImport((current) => current ? { ...current, state: "failed", error: message } : undefined);
        setNotice({ tone: "error", message });
        break;
      }
    }
  }

  function changeDecision(recordId: string, decision: Decision) {
    const before = workspace;
    setWorkspace((current) => updateLocalDecision(current, recordId, decision));
    if (!apiUrl) return;
    void fetch(`${apiUrl}/api/v1/reviews/${recordId}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ decision, note: "" }),
    }).then(async (response) => {
      if (!response.ok) throw new Error();
      setWorkspace(await response.json() as WorkspaceData);
    }).catch(() => {
      setWorkspace(before);
      setNotice({ tone: "error", message: "The review decision could not be saved." });
    });
  }

  return (
    <main className="single-workspace">
      <header className="workspace-topbar">
        <div className="brand-lockup"><span className="brand-mark">V</span><strong>VERITY</strong></div>
        <div className="topbar-tools">
          <button className="workspace-switcher" type="button">{workspace.dataset.name}<CaretDownIcon /></button>
          <span className="local-state"><i />Local</span>
          <DropdownMenu.Root>
            <DropdownMenu.Trigger><button className="icon-button" type="button" aria-label="Workspace options"><DotsThreeIcon /></button></DropdownMenu.Trigger>
            <DropdownMenu.Content align="end">
              <DropdownMenu.Item><ClockCounterClockwiseIcon />Run history</DropdownMenu.Item>
              <DropdownMenu.Item><GearSixIcon />Workflow settings</DropdownMenu.Item>
              <DropdownMenu.Item><PlugsConnectedIcon />Automation</DropdownMenu.Item>
            </DropdownMenu.Content>
          </DropdownMenu.Root>
        </div>
      </header>

      <section className="workspace-main">
        {notice ? <div className="operation-notice" data-tone={notice.tone} role="status">{notice.tone === "success" ? <CheckCircleIcon weight="fill" /> : <WarningCircleIcon weight="fill" />}{notice.message}<button type="button" aria-label="Dismiss notification" onClick={() => setNotice(null)}>×</button></div> : null}
        <header className="workspace-intro">
          <div><h1>See every transformation.</h1><p>Drop in raw data. Verity prepares it and shows exactly what changed.</p></div>
          <div className="intro-actions">
            {workspace.decision_breakdown.review > 0 ? <button className="review-button" type="button" onClick={() => setReviewOpen(true)}>Review {workspace.decision_breakdown.review} items</button> : null}
            <ResultAction workspace={workspace} apiUrl={apiUrl} />
          </div>
        </header>

        <div className="ingest-row">
          <ImportDropzone active={activeImport} disabled={busy} onFiles={(files) => void addFiles(files)} />
          <JobProgress active={activeImport} />
        </div>

        <WorkspaceView
          workspace={workspace}
          selectedStageId={selectedStageId}
          onStage={setSelectedStageId}
          apiUrl={apiUrl}
        />
        <PrivacyNote workspace={workspace} />
      </section>

      <ReviewDialog
        open={reviewOpen}
        onOpenChange={setReviewOpen}
        records={workspace.records.filter((record) => record.decision === "review")}
        total={workspace.decision_breakdown.review}
        onDecision={changeDecision}
      />
    </main>
  );
}

async function problemDetail(response: Response, fallback: string) {
  try {
    const problem = await response.json() as { detail?: string };
    return problem.detail || fallback;
  } catch {
    return fallback;
  }
}
