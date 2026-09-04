import { CaretDownIcon, CheckCircleIcon, QuestionIcon, XCircleIcon } from "@phosphor-icons/react";
import { DropdownMenu } from "@radix-ui/themes";
import type { Decision, EvidenceRecord } from "@/lib/contracts";
import { SourceMark } from "./source-mark";

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
    return (
      <div className="empty-state">
        <strong>No records match this view.</strong>
        <span>Choose another source or clear the active filter.</span>
      </div>
    );
  }

  return (
    <div className="record-table-wrap">
      <table className="record-table">
        <thead>
          <tr>
            <th>Source</th>
            <th>Input</th>
            <th>Output</th>
            <th>Confidence</th>
            <th>Decision</th>
          </tr>
        </thead>
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
                <span className="source-cell">
                  <SourceMark source={record.source} />
                  {record.source_label}
                </span>
              </td>
              <td className="record-input">{record.raw_event}</td>
              <td>{record.extracted_signal}</td>
              <td>
                <span className="confidence" data-level={record.confidence < 0.78 ? "review" : "ready"}>
                  {record.confidence.toFixed(2)}
                </span>
              </td>
              <td onClick={(event) => event.stopPropagation()}>
                <DropdownMenu.Root>
                  <DropdownMenu.Trigger>
                    <button className="decision-trigger" data-decision={record.decision} type="button">
                      <DecisionIcon decision={record.decision} />
                      {decisionLabel[record.decision]}
                      <CaretDownIcon size={13} />
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
