"use client";

import { memo, useState } from "react";
import { ArrowRightIcon, CaretDownIcon, ShieldCheckIcon } from "@phosphor-icons/react";
import { recordBody } from "./record-presentation";
import { diffText, stableValue, type ContentBlock, type ReadingDocument, type TableRecord, type View } from "./table-model";

const changeNames: Record<string, string> = { input: "Input", changed: "Modified", removed: "Removed", added: "Added", unchanged: "Unchanged", elsewhere: "Other branch" };
const roles: Record<string, string> = { user: "User prompt", assistant: "Agent reply", tool: "Tool result", tool_call: "Tool call", tool_result: "Tool result", message: "Message", email: "Email body", reviewer: "Review comment", before: "Before edit", after: "After edit", description: "Description", text: "Content" };

export function ContentReader({ rows, view, onRecord }: { rows: TableRecord[]; view: View; onRecord: (row: TableRecord) => void }) {
  return <div className="text-reader" aria-label="Readable record view">
    <div className="text-reader-intro"><div><span className="text-reader-kicker">Source content</span><strong>Read the actual exchange.</strong></div><span><ShieldCheckIcon size={14} /> Protected in every view, including Raw</span></div>
    <div className="text-record-list" key={view === "before" ? "before" : "current"}>
      {rows.map(row => <ContentRecord key={`${row.record_id}-${row.ordinal}`} row={row} view={view} onRecord={onRecord} />)}
      {!rows.length ? <div className="text-reader-empty">No records in this view. Try Changes or another filter.</div> : null}
    </div>
  </div>;
}

function fallbackDocument(row: TableRecord, side: "before" | "after"): ReadingDocument | undefined {
  if (!row[side]) return;
  const body = recordBody(row[side]);
  if (!body) return;
  const blocks = Object.entries(body).filter(([key, value]) => /^(content|message|text|body|description|prompt|response|summary|notes)$/.test(key) && typeof value === "string").map(([key, text]) => ({ id: key, role: key === "prompt" ? "user" : key === "response" ? "assistant" : "text", text: String(text), source_path: key }));
  return { title: typeof body.title === "string" ? body.title : typeof body.subject === "string" ? body.subject : undefined, blocks };
}

const ContentRecord = memo(function ContentRecord({ row, view, onRecord }: { row: TableRecord; view: View; onRecord: (row: TableRecord) => void }) {
  const [expanded, setExpanded] = useState(false);
  const before = row.before_document ?? fallbackDocument(row, "before");
  const after = row.after_document ?? fallbackDocument(row, "after");
  const doc = (view === "before" ? before : after ?? before)!;
  const earlier = new Map((before?.blocks ?? []).map(block => [block.id, block]));
  const later = new Map((after?.blocks ?? []).map(block => [block.id, block]));
  const blocks = [...(doc?.blocks ?? [])];
  if (view === "changes" && before && after) for (const block of before.blocks) if (!later.has(block.id)) blocks.push(block);
  let messages = 0;
  const displayed = expanded ? blocks : blocks.filter(block => block.role.startsWith("tool") || ++messages <= 4);
  const hidden = blocks.length - displayed.length;
  const body = recordBody(view === "before" ? row.before : row.after ?? row.before);
  const equalText = before && after && before.blocks.length === after.blocks.length && before.blocks.every(block => later.get(block.id)?.text === block.text);
  const sourceChanged = before && after && before.blocks.some(block => later.get(block.id)?.source_path !== block.source_path);
  const mappingOnly = view === "changes" && row.change === "changed" && equalText;
  const signal = typeof body?.extracted_signal === "string" ? body.extracted_signal : "";

  return <article className="text-record conversation-record" data-record-id={row.record_id} data-change={row.change}>
    <header className="text-record-head">
      <div className="text-record-context"><span className="reader-source">{doc?.source || "Source record"}</span><span className="text-change" data-kind={row.change}>{changeNames[row.change]}</span>{doc?.synthetic ? <span className="reader-sample">Synthetic sample</span> : null}<span className="reader-entry-count">{blocks.length ? `${blocks.length} content ${blocks.length === 1 ? "entry" : "entries"}` : "Structured fields"}</span></div>
      <button className="text-inspect" type="button" onClick={() => onRecord(row)}>Inspect fields <ArrowRightIcon size={14} /></button>
    </header>
    {doc?.title ? <h3 className="text-record-title">{doc.title}</h3> : null}
    {mappingOnly ? <p className="reader-mapping">Content preserved · {sourceChanged ? "fields mapped" : "structure or metadata changed"}<span>Compare the fields in Inspect.</span></p> : null}
    <div className="conversation-turns">
      {displayed.map((block, index) => {
        const previous = earlier.get(block.id), next = later.get(block.id);
        const state = view !== "changes" || row.change === "input" ? "same" : !next ? "removed" : !previous ? "added" : previous.text !== next.text ? "changed" : "same";
        return <MessageBlock key={block.id} block={block} previous={previous} next={next} index={index} state={state} />;
      })}
    </div>
    {hidden > 0 || expanded && blocks.filter(block => !block.role.startsWith("tool")).length > 4 ? <button className="reader-expand" type="button" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>{expanded ? "Show fewer messages" : `Show ${hidden} more ${hidden === 1 ? "message" : "messages"}`}<CaretDownIcon size={13} /></button> : null}
    {!blocks.length ? <dl className="reader-structured">{Object.entries(body ?? {}).filter(([key]) => !["metadata", "attributes", "content_fingerprint", "dataset_id", "batch_id", "record_id", "event_id"].includes(key)).slice(0, 12).map(([key, value]) => <div key={key}><dt>{key.replaceAll("_", " ")}</dt><dd>{stableValue(value)}</dd></div>)}</dl> : null}
    {signal ? <details className="reader-signal"><summary>Extracted feedback · source quote</summary><blockquote>{signal}</blockquote><p>Candidate for review, not a verified outcome.</p></details> : null}
    <footer className="reader-record-footer"><details><summary>Transformation & provenance</summary><p>{row.reason || (row.change === "input" ? "Original text fields, with display protection applied. No summary is substituted." : "Compare this stage with its preceding snapshot. Removed records remain in the input.")}</p><code>{row.record_id}</code></details></footer>
  </article>;
});

function MessageBlock({ block, previous, next, index, state }: { block: ContentBlock; previous?: ContentBlock; next?: ContentBlock; index: number; state: string }) {
  const [full, setFull] = useState(false);
  const tool = block.role.startsWith("tool");
  const correction = ["correction", "steer", "interrupt"].includes(block.interaction ?? "");
  const label = correction ? block.interaction === "interrupt" ? "User interruption" : "User correction" : roles[block.role] ?? block.role;
  const beforeText = previous?.text ?? "", afterText = next?.text ?? "";
  const long = Math.max(block.text.length, beforeText.length, afterText.length) > 1400;
  const content = <>
    <div className="text-field message-copy" data-folded={long && !full}>
      <p>{state === "changed" ? diffText(beforeText, afterText).map((part, i) => part.kind === "same" ? <span key={i}>{part.text}</span> : <mark key={i} data-diff={part.kind}>{part.text}</mark>) : state === "added" || state === "removed" ? <mark data-diff={state}>{block.text}</mark> : block.text}</p>
    </div>
    {long ? <button className="reader-expand" type="button" aria-expanded={full} onClick={() => setFull(!full)}>{full ? "Collapse text" : "Read full text"}</button> : null}
    <details className="message-provenance"><summary>Source field{block.reply_to ? " · linked reply" : ""}</summary><code>{block.source_path}</code>{block.reply_to ? <span>Reply to {block.reply_to}</span> : null}{block.format === "html" ? <span>HTML shown as plain text. Original markup in Inspect.</span> : null}</details>
  </>;
  return tool ? <details className="message-tool" data-change={state}><summary><span className="message-role">{label}</span><span>{block.tool || "Execution output"}</span><CaretDownIcon size={13} /></summary>{content}</details> : <section className="message-block" data-role={block.role} data-correction={correction} data-change={state}>
    <div className="message-heading"><span className="message-role">{label}</span>{correction ? <span className="correction-note">Direction changed</span> : null}<span className="message-index">{String(index + 1).padStart(2, "0")}</span></div>
    {content}
  </section>;
}
