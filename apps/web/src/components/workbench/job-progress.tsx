import { CheckCircleIcon, CircleNotchIcon, WarningCircleIcon } from "@phosphor-icons/react";
import type { ActiveImport } from "./workspace-machine";
import { stateLabel } from "./workspace-machine";

export function JobProgress({ active }: { active?: ActiveImport }) {
  if (!active) return null;
  const failed = active.state === "failed";
  const ready = active.state === "ready";
  return (
    <div className="job-state" data-state={active.state} role="status" aria-live="polite">
      {failed ? <WarningCircleIcon weight="fill" /> : ready ? <CheckCircleIcon weight="fill" /> : <CircleNotchIcon className="job-spinner" />}
      <span><strong>{stateLabel(active)}</strong>{active.error ? <small>{active.error}</small> : null}</span>
    </div>
  );
}
