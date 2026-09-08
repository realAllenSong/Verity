"use client";

import { useState } from "react";
import { Dialog } from "@radix-ui/themes";
import { CopyIcon, XIcon } from "@phosphor-icons/react";
import type { WorkspaceData } from "@/lib/contracts";

export type DetailView = "Run history" | "Workflow details" | "Automation";

export function WorkspaceDetails({ view, workspace, apiUrl, onClose }: { view: DetailView | null; workspace: WorkspaceData; apiUrl?: string; onClose: () => void }) {
  const [copied, setCopied] = useState("");
  const command = `./bin/verity import ./data.csv --server ${apiUrl || "http://127.0.0.1:8000"} --wait --output ./ready.parquet`;
  return <Dialog.Root open={Boolean(view)} onOpenChange={(open) => { if (!open) onClose(); }}>
    <Dialog.Content className="verity-dialog" maxWidth="720px">
      <div className="dialog-heading"><div><Dialog.Title>{view}</Dialog.Title><Dialog.Description>{view === "Run history" ? "Recorded executions in this workspace." : view === "Workflow details" ? "The recipe applied to your data." : "Use the same pipeline from your own tools."}</Dialog.Description></div><Dialog.Close><button className="icon-button" type="button" aria-label="Close"><XIcon /></button></Dialog.Close></div>
      <div className="workspace-details">
        {view === "Run history" ? <div className="detail-list">{(workspace.run_events ?? []).map((event, index) => <div key={`${event.run_id}-${event.event_type}-${index}`}><strong>{event.event_type === "COMPLETE" ? "Completed" : event.event_type === "FAIL" ? "Failed" : "Started"}</strong><code>{event.run_id}</code><time>{event.event_time.replace("T", " ").replace("Z", " UTC")}</time></div>)}</div> : null}
        {view === "Workflow details" ? <>
          <p className="detail-intro">{workspace.recipe.name}. This installation uses the workflow-signal recipe; new domain rules are configured in the Go engine.</p>
          <ol className="detail-list">{workspace.recipe.operators.map((operator) => <li key={operator.id}><strong>{operator.label}</strong><span>{operator.description}</span><code>{operator.operator} / {operator.version}</code></li>)}</ol>
          <details><summary>Run provenance</summary><pre>{JSON.stringify(workspace.step_settings, null, 2)}</pre></details>
        </> : null}
        {view === "Automation" ? <>
          <h3>Command line</h3><p>Build once with <code>make cli</code>, then import and download in one command.</p><pre>{command}</pre>
          <button className="detail-copy" type="button" onClick={async () => { try { await navigator.clipboard.writeText(command); setCopied("Copied"); } catch { setCopied("Select and copy the command above"); } }}><CopyIcon />{copied || "Copy command"}</button>
          <h3>REST API</h3><p>Create an import, upload its bytes, then finalize. Processing starts automatically.</p>{apiUrl ? <a href={`${apiUrl}/openapi.json`} target="_blank" rel="noreferrer">Open API specification</a> : null}
          <h3>Agent access</h3><p>Build with <code>make mcp</code>. Configure your agent to launch <code>bin/verity-mcp</code> with <code>VERITY_SERVER</code> set to this API.</p>
        </> : null}
      </div>
    </Dialog.Content>
  </Dialog.Root>;
}
