import type { CSSProperties } from "react";
import type { PipelineStage } from "@/lib/contracts";

interface PipelineFlowProps {
  stages: PipelineStage[];
  selectedStage: string;
  onSelect: (stageId: string) => void;
}

export function PipelineFlow({ stages, selectedStage, onSelect }: PipelineFlowProps) {
  const largest = Math.max(...stages.map((stage) => stage.count));
  const ready = stages.find((stage) => stage.id === "curated");

  return (
    <div className="pipeline-flow" aria-label="Pipeline stages">
      <div className="pipeline-stage-grid">
        {stages.map((stage) => (
          <button
            key={stage.id}
            className="pipeline-stage"
            data-selected={selectedStage === stage.id}
            type="button"
            onClick={() => onSelect(stage.id)}
            aria-pressed={selectedStage === stage.id}
            data-status={stage.status}
          >
            <span>{stage.label}</span>
            <strong>{stage.count.toLocaleString()}</strong>
          </button>
        ))}
      </div>
      <div className="throughput-grid" aria-hidden="true">
        {stages.map((stage, index) => {
          const next = stages[index + 1] ?? stage;
          const isReviewBranch = stage.id === "review";
          const flowCount = isReviewBranch && ready ? ready.count : stage.count;
          const nextCount = next.id === "review" && ready ? ready.count : next.count;
          const from = Math.max(4, Math.round((flowCount / largest) * 54));
          const to = Math.max(4, Math.round((nextCount / largest) * 54));
          const style = { "--flow-from": `${from}px`, "--flow-to": `${to}px` } as CSSProperties;
          return (
            <div
              key={stage.id}
              className="throughput-segment"
              data-stage={stage.id}
              data-selected={selectedStage === stage.id}
              style={style}
            />
          );
        })}
      </div>
    </div>
  );
}
