import type { WorkspaceData } from "@/lib/contracts";

export function DistributionView({ data }: { data: WorkspaceData["signal_distribution"] }) {
  const values = Object.entries(data).sort((left, right) => right[1] - left[1]);
  const max = Math.max(...values.map(([, value]) => value));
  return (
    <div className="distribution-view">
      {values.map(([name, value]) => (
        <div className="distribution-row" key={name}>
          <span>{name.replaceAll("_", " ")}</span>
          <div className="distribution-line" style={{ width: `${Math.max(10, (value / max) * 100)}%` }} />
          <strong>{value.toLocaleString()}</strong>
        </div>
      ))}
    </div>
  );
}

export function SchemaView({ workspace }: { workspace: WorkspaceData }) {
  return (
    <div className="schema-view">
      <section>
        <h3>Before</h3>
        {workspace.schema_before.map((field) => (
          <div className="schema-row" key={field.field}>
            <code>{field.field}</code><span>{field.type}</span><small>{field.policy}</small>
          </div>
        ))}
      </section>
      <section>
        <h3>After</h3>
        {workspace.schema_after.map((field) => (
          <div className="schema-row" key={field.field}>
            <code>{field.field}</code><span>{field.type}</span><small>{field.policy}</small>
          </div>
        ))}
      </section>
    </div>
  );
}
