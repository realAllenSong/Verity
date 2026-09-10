"use client";

import { useState } from "react";
import {
  CheckCircleIcon,
  ClockCounterClockwiseIcon,
  DotsThreeIcon,
  GearSixIcon,
  PlugsConnectedIcon,
  ShieldCheckIcon,
  WarningCircleIcon,
} from "@phosphor-icons/react";
import { DropdownMenu } from "@radix-ui/themes";
import type { Decision, EvidenceRecord, ImportSummary, JobEvent, JobSummary, WorkspaceData } from "@/lib/contracts";
import { ImportDropzone } from "./import-dropzone";
import { JobProgress } from "./job-progress";
import type { ActiveImport } from "./workspace-machine";
import { screenState } from "./workspace-machine";
import { PrivacyNote, ResultAction, ReviewDialog, WorkspaceView } from "./workspace-view";
import { WorkspaceDetails, type DetailView } from "./workspace-details";

const chunkBytes = 8 * 1024 * 1024;

type Notice = { tone: "success" | "error"; message: string } | null;

export function Workbench({ initialWorkspace }: { initialWorkspace: WorkspaceData }) {
  const [workspace, setWorkspace] = useState(initialWorkspace);
  const [selectedStageId, setSelectedStageId] = useState("raw");
  const [activeImport, setActiveImport] = useState<ActiveImport>();
  const [reviewOpen, setReviewOpen] = useState(false);
  const [reviewRecords, setReviewRecords] = useState<EvidenceRecord[]>([]);
  const [reviewBusy, setReviewBusy] = useState(false);
  const [reviewError, setReviewError] = useState("");
  const [details, setDetails] = useState<DetailView | null>(null);
  const [notice, setNotice] = useState<Notice>(null);
  const [inspectionQuery, setInspectionQuery] = useState("");
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
    let latest = job;
    const events = typeof EventSource === "undefined" ? undefined : new EventSource(`${apiUrl}${job.events_url}`);
    events?.addEventListener("stage_committed", (message) => {
      const event = JSON.parse((message as MessageEvent<string>).data) as JobEvent;
      setActiveImport((active) => active ? { ...active, stage: event.stage_id } : active);
    });
    try {
      while (true) {
        const response = await fetch(`${apiUrl}${job.status_url}`, { cache: "no-store" });
        if (!response.ok) throw new Error("Processing status is unavailable.");
        latest = await response.json() as JobSummary;
        const state = screenState(latest);
        setActiveImport((active) => ({ ...current, uploaded: current.bytes, stage: active?.stage, state, job: latest, error: latest.error }));
        if (latest.state === "succeeded") {
          await refreshWorkspace();
          setSelectedStageId("raw");
          setNotice({ tone: "success", message: "Result ready. Every stage can now be inspected." });
          return;
        }
        if (latest.state === "failed" || latest.state === "canceled") {
          throw new Error(latest.error || "Processing failed. The previous result is unchanged.");
        }
        await new Promise((resolve) => setTimeout(resolve, 1000));
      }
    } finally {
      events?.close();
    }
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
    setInspectionQuery("");
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

  async function tryLanguageSample() {
    try {
      const sample = await import("../../../../../sample_data/examples/language-workflow.json");
      await uploadFile(new File([JSON.stringify(sample.default)], "language-workflow.json", { type: "application/json", lastModified: 0 }));
      setInspectionQuery("language-demo");
    } catch (error) {
      const message = error instanceof Error ? error.message : "The sample could not be imported.";
      setActiveImport(current => current ? { ...current, state: "failed", error: message } : undefined);
      setNotice({ tone: "error", message });
    }
  }

  async function loadReviews() {
    if (!apiUrl) throw new Error("Start the API to review records.");
    const response = await fetch(`${apiUrl}/api/v1/review-queue`, { cache: "no-store" });
    if (!response.ok) throw new Error("The review queue could not be loaded.");
    const queue = await response.json() as { records: EvidenceRecord[] };
    setReviewRecords(queue.records);
  }

  async function openReview() {
    setReviewOpen(true);
    setReviewBusy(true);
    setReviewError("");
    try { await loadReviews(); } catch (error) { setReviewError((error as Error).message); }
    finally { setReviewBusy(false); }
  }

  async function changeDecision(recordId: string, decision: Decision) {
    if (!apiUrl || reviewBusy) return;
    setReviewBusy(true);
    setReviewError("");
    try {
      const response = await fetch(`${apiUrl}/api/v1/reviews/${encodeURIComponent(recordId)}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ decision, note: decision === "accepted" ? "Accepted after source review in the workbench." : "Excluded after source review in the workbench." }),
      });
      if (!response.ok) throw new Error("The review decision could not be saved. Try again.");
      setWorkspace(await response.json() as WorkspaceData);
      await loadReviews();
    } catch (error) { setReviewError((error as Error).message); }
    finally { setReviewBusy(false); }
  }

  return (
    <main className="single-workspace">
      <header className="workspace-topbar">
        <div className="brand-lockup"><span className="brand-mark">V</span><strong>VERITY</strong></div>
        <div className="topbar-tools">
          <span className="workspace-name">{workspace.dataset.name}</span>
          <span className="local-state"><ShieldCheckIcon size={15} />Self-hosted</span>
          <DropdownMenu.Root>
            <DropdownMenu.Trigger><button className="icon-button" type="button" aria-label="Workspace options"><DotsThreeIcon /></button></DropdownMenu.Trigger>
            <DropdownMenu.Content align="end">
              <DropdownMenu.Item onSelect={() => setDetails("Run history")}><ClockCounterClockwiseIcon />Run history</DropdownMenu.Item>
              <DropdownMenu.Item onSelect={() => setDetails("Workflow details")}><GearSixIcon />Workflow details</DropdownMenu.Item>
              <DropdownMenu.Item onSelect={() => setDetails("Automation")}><PlugsConnectedIcon />Automation</DropdownMenu.Item>
            </DropdownMenu.Content>
          </DropdownMenu.Root>
        </div>
      </header>

      <section className="workspace-main">
        {notice ? <div className="operation-notice" data-tone={notice.tone} role="status">{notice.tone === "success" ? <CheckCircleIcon weight="fill" /> : <WarningCircleIcon weight="fill" />}{notice.message}<button type="button" aria-label="Dismiss notification" onClick={() => setNotice(null)}>×</button></div> : null}
        <header className="workspace-intro">
          <div><h1>See every transformation.</h1><p>Drop in raw data. Follow every change, from input to result.</p></div>
          <div className="intro-actions">
            {workspace.decision_breakdown.review > 0 ? <button className="review-button" type="button" onClick={() => void openReview()}>Review {workspace.decision_breakdown.review} items</button> : null}
            <ResultAction workspace={workspace} apiUrl={apiUrl} />
          </div>
        </header>

        <div className="ingest-row">
          <ImportDropzone active={activeImport} disabled={busy} onFiles={(files) => void addFiles(files)} />
          <JobProgress active={activeImport} />
        </div>
        <div className="language-sample-entry"><button type="button" disabled={busy} onClick={() => void tryLanguageSample()}>Try a conversation sample <span>↗</span></button><span>13 synthetic records · prompts, replies, reviews & document edits</span></div>

        <WorkspaceView
          workspace={workspace}
          selectedStageId={selectedStageId}
          onStage={setSelectedStageId}
          apiUrl={apiUrl}
          inspectionQuery={inspectionQuery}
          onQuery={setInspectionQuery}
        />
        <PrivacyNote workspace={workspace} />
      </section>

      <ReviewDialog
        open={reviewOpen}
        onOpenChange={setReviewOpen}
        records={reviewRecords}
        total={workspace.decision_breakdown.review}
        busy={reviewBusy}
        error={reviewError}
        onRetry={() => void openReview()}
        onDecision={changeDecision}
      />
      <WorkspaceDetails view={details} workspace={workspace} apiUrl={apiUrl} onClose={() => setDetails(null)} />
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
