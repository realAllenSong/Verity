import type { PipelineStage } from "@/lib/contracts";

interface PipelineFlowProps {
  stages: PipelineStage[];
  selectedStage: string;
  onSelect: (stageId: string) => void;
  isRunning: boolean;
}

export function PipelineFlow({ stages, selectedStage, onSelect, isRunning }: PipelineFlowProps) {
  const core = stages.filter((stage) => !["review", "curated"].includes(stage.id));
  const review = stages.find((stage) => stage.id === "review");
  const ready = stages.find((stage) => stage.id === "curated");

  return (
    <div className="pipeline-flow" aria-label="Pipeline stages" data-running={isRunning}>
      {isRunning ? <div className="run-progress" role="status"><span>Processing locally</span><i /></div> : null}
      <div className="pipeline-core">
        {core.map((stage, index) => (
          <div className="pipeline-step-wrap" key={stage.id}>
            <button className="pipeline-stage" data-selected={selectedStage === stage.id} type="button" onClick={() => onSelect(stage.id)} aria-pressed={selectedStage === stage.id}>
              <span>{stage.label}</span><strong>{stage.count.toLocaleString()}</strong>
            </button>
            {index < core.length - 1 ? <i aria-hidden="true" /> : null}
          </div>
        ))}
      </div>
      <div className="pipeline-branch" aria-label="Pipeline results">
        {review ? (
          <button className="branch-stage review-branch" data-selected={selectedStage === review.id} type="button" onClick={() => onSelect(review.id)}>
            <span>Review</span><strong>{review.count.toLocaleString()}</strong>
          </button>
        ) : null}
        {ready ? (
          <button className="branch-stage ready-branch" data-selected={selectedStage === ready.id} type="button" onClick={() => onSelect(ready.id)}>
            <span>Ready</span><strong>{ready.count.toLocaleString()}</strong>
          </button>
        ) : null}
      </div>
    </div>
  );
}
