import { CaretDownIcon, CaretRightIcon, CheckCircleIcon, QuestionIcon, XCircleIcon } from "@phosphor-icons/react";
import { DropdownMenu } from "@radix-ui/themes";
import type { Decision, EvidenceRecord } from "@/lib/contracts";

interface RecordTableProps {
  records: EvidenceRecord[];
  selectedRecordId: string | null;
  onOpenRecord: (record: EvidenceRecord) => void;
  onDecision: (recordId: string, decision: Decision) => void;
}

const decisionLabel: Record<Decision, string> = {
  accepted: "Accepted",
  rejected: "Rejected",
  modified: "Modified",
  review: "Review",
};

function DecisionIcon({ decision }: { decision: Decision }) {
  if (decision === "review") return <QuestionIcon size={16} weight="bold" />;
  if (decision === "rejected") return <XCircleIcon size={16} weight="bold" />;
  return <CheckCircleIcon size={16} weight="bold" />;
}

export function RecordTable({ records, selectedRecordId, onOpenRecord, onDecision }: RecordTableProps) {
  if (!records.length) {
    return <div className="empty-state"><strong>No records match this view.</strong><span>Choose another stage or clear the active filter.</span></div>;
  }

  return (
    <div className="record-table-wrap">
      <table className="record-table">
        <thead><tr><th>Record</th><th>Transformation</th><th>Quality</th><th>Decision</th></tr></thead>
        <tbody>
          {records.map((record) => (
            <tr
              key={record.id}
              data-selected={selectedRecordId === record.id}
              onClick={() => onOpenRecord(record)}
              tabIndex={0}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") onOpenRecord(record);
              }}
            >
              <td>
                <span className="record-cell">
                  <CaretRightIcon size={14} />
                  <span><code>{record.id.replace("evt_", "rec_")}</code><small>{record.signal_type.replaceAll("_", " ")}</small></span>
                </span>
              </td>
              <td><TransformationPreview before={record.before_fields} after={record.after_fields} /></td>
              <td><span className="quality-cell"><strong>{Math.round(record.quality_score * 100)}%</strong><i><b style={{ width: `${record.quality_score * 100}%` }} /></i></span></td>
              <td onClick={(event) => event.stopPropagation()}>
                <DropdownMenu.Root>
                  <DropdownMenu.Trigger>
                    <button className="decision-trigger" data-decision={record.decision} type="button">
                      <DecisionIcon decision={record.decision} />{decisionLabel[record.decision]}<CaretDownIcon size={13} />
                    </button>
                  </DropdownMenu.Trigger>
                  <DropdownMenu.Content align="end" size="1">
                    <DropdownMenu.Item onSelect={() => onDecision(record.id, "accepted")}>Accept</DropdownMenu.Item>
                    <DropdownMenu.Item onSelect={() => onDecision(record.id, "modified")}>Mark modified</DropdownMenu.Item>
                    <DropdownMenu.Separator />
                    <DropdownMenu.Item color="red" onSelect={() => onDecision(record.id, "rejected")}>Reject</DropdownMenu.Item>
                  </DropdownMenu.Content>
                </DropdownMenu.Root>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function TransformationPreview({ before, after }: { before: Record<string, string>; after: Record<string, string> }) {
  const [beforeKey, beforeValue] = Object.entries(before)[0] ?? ["input", "-"];
  const [afterKey, afterValue] = Object.entries(after)[0] ?? ["output", "-"];
  return (
    <span className="transformation-preview">
      <span><small>{beforeKey}</small><code>{beforeValue}</code></span>
      <span aria-hidden="true">→</span>
      <span><small>{afterKey}</small><code>{afterValue}</code></span>
    </span>
  );
}
