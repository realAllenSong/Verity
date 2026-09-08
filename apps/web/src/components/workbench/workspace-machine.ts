import type { JobSummary } from "@/lib/contracts";

export type WorkspaceScreenState = "ready" | "uploading" | "processing" | "needs_input" | "failed";

export interface ActiveImport {
  filename: string;
  bytes: number;
  uploaded: number;
  state: WorkspaceScreenState;
  job?: JobSummary;
  stage?: string;
  error?: string;
}

export function screenState(job?: JobSummary): WorkspaceScreenState {
  if (!job) return "ready";
  if (job.state === "failed" || job.state === "canceled") return "failed";
  if (job.state === "needs_input") return "needs_input";
  if (job.state === "succeeded") return "ready";
  return "processing";
}

export function stateLabel(active?: ActiveImport) {
  if (!active) return "Ready for data";
  if (active.state === "uploading") return "Uploading";
  if (active.state === "processing") return active.stage ? `${stageLabel(active.stage)} complete` : "Processing locally";
  if (active.state === "needs_input") return "Review needed";
  if (active.state === "failed") return "Needs attention";
  return "Result ready";
}

function stageLabel(stage: string) {
  return ({ raw: "Raw", normalize: "Normalize", privacy: "Privacy", quality: "Quality", signals: "Extract", review: "Review", curated: "Ready" } as Record<string, string>)[stage] ?? stage;
}
