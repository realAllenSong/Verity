import {
  ArrowSquareOutIcon,
  CheckCircleIcon,
  DownloadSimpleIcon,
  LockKeyIcon,
  XIcon,
} from "@phosphor-icons/react";
import { Dialog } from "@radix-ui/themes";
import type { EvidenceRecord, PipelineStage, SourceSummary, WorkspaceData } from "@/lib/contracts";
import { SourceMark } from "./source-mark";

export type DialogName = "sources" | "recipe" | "step" | "record" | "review" | "export" | null;

interface WorkspaceDialogsProps {
  active: DialogName;
  onChange: (dialog: DialogName) => void;
  workspace: WorkspaceData;
  stage: PipelineStage;
  record: EvidenceRecord | null;
  onAccept: (recordId: string) => void;
  onDownload: () => void;
}

function ModalShell({
  open,
  onOpenChange,
  title,
  description,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="verity-dialog" maxWidth="720px">
        <div className="dialog-heading">
          <div>
            <Dialog.Title>{title}</Dialog.Title>
            <Dialog.Description>{description}</Dialog.Description>
          </div>
          <Dialog.Close>
            <button className="icon-button" type="button" aria-label="Close dialog"><XIcon size={18} /></button>
          </Dialog.Close>
        </div>
        {children}
      </Dialog.Content>
    </Dialog.Root>
  );
}

function SourcesDialog({ sources, open, onOpenChange }: { sources: SourceSummary[]; open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <ModalShell open={open} onOpenChange={onOpenChange} title="Sources" description="Every connector maps into the same raw-event contract.">
      <div className="dialog-list source-dialog-list">
        {sources.map((source) => (
          <div className="dialog-list-row" key={source.id}>
            <span className="source-cell"><SourceMark source={source.id} /><strong>{source.label}</strong></span>
            <span className="row-detail">{source.count.toLocaleString()} events</span>
            <span className="status-word"><CheckCircleIcon size={16} weight="fill" /> Healthy</span>
          </div>
        ))}
      </div>
      <div className="dialog-note">
        <LockKeyIcon size={18} />
        <span>Source adapters are pluggable. These nine connectors are synthetic examples, not a fixed product boundary.</span>
      </div>
    </ModalShell>
  );
}

function RecipeDialog({ stages, open, onOpenChange }: { stages: PipelineStage[]; open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <ModalShell open={open} onOpenChange={onOpenChange} title="Recipe" description="A versioned sequence of operators and review gates.">
      <div className="recipe-dialog-list">
        {stages.map((stage, index) => (
          <div className="recipe-dialog-row" key={stage.id}>
            <span className="recipe-index">{String(index + 1).padStart(2, "0")}</span>
            <span><strong>{stage.label}</strong><small>{stage.description}</small></span>
            <code>{stage.operator}</code>
            <strong className="row-count">{stage.count.toLocaleString()}</strong>
          </div>
        ))}
      </div>
    </ModalShell>
  );
}

function StepDialog({ workspace, stage, open, onOpenChange }: { workspace: WorkspaceData; stage: PipelineStage; open: boolean; onOpenChange: (open: boolean) => void }) {
  const retained = stage.input_count ? (stage.count / stage.input_count) * 100 : 100;
  return (
    <ModalShell open={open} onOpenChange={onOpenChange} title={stage.label} description={stage.description}>
      <div className="step-summary">
        <div><span>Input</span><strong>{stage.input_count.toLocaleString()}</strong></div>
        <div><span>Output</span><strong>{stage.count.toLocaleString()}</strong></div>
        <div><span>Retained</span><strong>{retained.toFixed(1)}%</strong></div>
      </div>
      <dl className="property-list">
        <div><dt>Operator</dt><dd><code>{stage.operator}</code></dd></div>
        <div><dt>Operator version</dt><dd>{workspace.step_settings.version}</dd></div>
        <div><dt>Policy</dt><dd>{workspace.step_settings.policy}</dd></div>
        <div><dt>Confidence threshold</dt><dd>{workspace.step_settings.threshold}</dd></div>
        <div><dt>Input snapshot</dt><dd><code>{workspace.step_settings.input_snapshot}</code></dd></div>
        <div><dt>Run</dt><dd><code>{workspace.run_id}</code></dd></div>
      </dl>
      <button className="text-action" type="button">View lineage <ArrowSquareOutIcon size={16} /></button>
    </ModalShell>
  );
}

function RecordDialog({ record, open, onOpenChange }: { record: EvidenceRecord | null; open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <ModalShell open={open} onOpenChange={onOpenChange} title="Record evidence" description="A local-safe explanation of one pipeline decision.">
      {record ? (
        <div className="record-dialog-body">
          <div className="record-compare">
            <div><span>Input</span><p>{record.raw_event}</p></div>
            <div><span>Output</span><p>{record.extracted_signal}</p></div>
          </div>
          <dl className="property-list compact">
            <div><dt>Source</dt><dd>{record.source_label}</dd></div>
            <div><dt>Confidence</dt><dd>{record.confidence.toFixed(2)}</dd></div>
            <div><dt>Evidence</dt><dd>{record.evidence_count} matched events</dd></div>
            <div><dt>Reason</dt><dd>{record.reason}</dd></div>
          </dl>
          <div className="dialog-note"><LockKeyIcon size={18} /><span>Raw prompts, files, and message bodies are not included in this shareable view.</span></div>
        </div>
      ) : null}
    </ModalShell>
  );
}

function ReviewDialog({ records, open, onOpenChange, onAccept }: { records: EvidenceRecord[]; open: boolean; onOpenChange: (open: boolean) => void; onAccept: (recordId: string) => void }) {
  return (
    <ModalShell open={open} onOpenChange={onOpenChange} title="Review queue" description="Only uncertain decisions appear here.">
      {records.length ? (
        <div className="review-list">
          {records.slice(0, 6).map((record) => (
            <div className="review-row" key={record.id}>
              <SourceMark source={record.source} />
              <span><strong>{record.extracted_signal}</strong><small>{record.reason}</small></span>
              <code>{record.confidence.toFixed(2)}</code>
              <button type="button" onClick={() => onAccept(record.id)}>Accept</button>
            </div>
          ))}
        </div>
      ) : (
        <div className="empty-state"><strong>Queue cleared.</strong><span>No sample records need review.</span></div>
      )}
    </ModalShell>
  );
}

function ExportDialog({ workspace, open, onOpenChange, onDownload }: { workspace: WorkspaceData; open: boolean; onOpenChange: (open: boolean) => void; onDownload: () => void }) {
  const ready = workspace.stages.find((stage) => stage.id === "curated")?.count ?? 0;
  return (
    <ModalShell open={open} onOpenChange={onOpenChange} title="Export" description="Publish a reproducible snapshot without local-only content.">
      <div className="export-summary"><strong>{ready.toLocaleString()}</strong><span>curated records ready</span></div>
      <div className="export-options">
        <button type="button" onClick={onDownload}><DownloadSimpleIcon size={18} /> Download manifest</button>
        <span>Parquet and JSONL exporters use the same snapshot contract.</span>
      </div>
    </ModalShell>
  );
}

export function WorkspaceDialogs({ active, onChange, workspace, stage, record, onAccept, onDownload }: WorkspaceDialogsProps) {
  const change = (name: DialogName) => (open: boolean) => onChange(open ? name : null);
  return (
    <>
      <SourcesDialog sources={workspace.sources} open={active === "sources"} onOpenChange={change("sources")} />
      <RecipeDialog stages={workspace.stages} open={active === "recipe"} onOpenChange={change("recipe")} />
      <StepDialog workspace={workspace} stage={stage} open={active === "step"} onOpenChange={change("step")} />
      <RecordDialog record={record} open={active === "record"} onOpenChange={change("record")} />
      <ReviewDialog records={workspace.records.filter((item) => item.decision === "review")} open={active === "review"} onOpenChange={change("review")} onAccept={onAccept} />
      <ExportDialog workspace={workspace} open={active === "export"} onOpenChange={change("export")} onDownload={onDownload} />
    </>
  );
}
