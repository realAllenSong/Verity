"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Dialog, Popover } from "@radix-ui/themes";
import { ArrowRightIcon, ArrowsOutSimpleIcon, ColumnsIcon, MagnifyingGlassIcon, PlayIcon, TableIcon, XIcon } from "@phosphor-icons/react";
import { recordBody } from "./record-presentation";
import { cellChange, diffText, groupRows, orderedFields, stableValue, textFields, visibleRows, type TablePage, type TableRecord, type View } from "./table-model";

const labels: Record<string, string> = { raw: "Raw", normalize: "Normalize", privacy: "Privacy", quality: "Quality", signals: "Extract", review: "Review", curated: "Ready" };
const changes: Record<string, string> = { input: "Input", changed: "Modified", removed: "Removed", added: "Added", unchanged: "Unchanged", elsewhere: "Other branch" };
const fullValue = (value: unknown) => value === "" ? '""' : stableValue(value);
const preview = (value: unknown) => { const text = fullValue(value); return text.length > 220 ? text.slice(0, 220) + "…" : text; };

export function TransformationTable({ stage, runID, revision, apiUrl }: { stage: string; runID: string; revision: string; apiUrl?: string }) {
  // Remounted by run/stage/review snapshot in the parent: stale records never
  // appear under a newly selected boundary heading.
  const [filter, setFilter] = useState(stage === "review" || stage === "curated" ? "result" : "all");
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [cursors, setCursors] = useState([""]);
  const [pageIndex, setPageIndex] = useState(0);
  const [retry, setRetry] = useState(0);
  const [result, setResult] = useState<{ key: string; page?: TablePage; error?: string }>();
  const [view, setView] = useState<View>(stage === "raw" ? "after" : "changes");
  const [surface, setSurface] = useState<"reading" | "table">("reading");
  const [chosen, setChosen] = useState<string[] | null>(null);
  const [columnQuery, setColumnQuery] = useState("");
  const [wrap, setWrap] = useState(false);
  const [group, setGroup] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [replay, setReplay] = useState(0);
  const [phase, setPhase] = useState<"before" | "settled">("settled");
  const [cell, setCell] = useState<{ row: TableRecord; field: string } | null>(null);
  const [record, setRecord] = useState<TableRecord | null>(null);
  const scroll = useRef<HTMLDivElement>(null);
  const cursor = cursors[pageIndex];
  const key = `${runID}:${revision}:${stage}:${filter}:${search}:${cursor}:${retry}`;
  const loading = result?.key !== key;
  const page = !loading ? result?.page : undefined;
  const error = !loading ? result?.error : undefined;

  useEffect(() => {
    const timer = setTimeout(() => { setSearch(query.trim()); setCursors([""]); setPageIndex(0); setCell(null); }, 250);
    return () => clearTimeout(timer);
  }, [query]);
  useEffect(() => {
    if (!apiUrl) return;
    const abort = new AbortController();
    const params = new URLSearchParams({ run_id: runID, limit: "100", cursor, filter, q: search });
    void fetch(`${apiUrl}/api/v1/stages/${stage}/table?${params}`, { cache: "no-store", signal: abort.signal })
      .then(async response => { if (!response.ok) throw new Error(response.status === 409 ? "This snapshot changed. Refresh the workspace to inspect the current result." : "This table could not be read. Your data is unchanged."); return response.json() as Promise<TablePage>; })
      .then(page => { if (!abort.signal.aborted) setResult({ key, page }); })
      .catch((error: Error) => { if (!abort.signal.aborted) setResult({ key, error: error.message }); });
    return () => abort.abort();
  }, [apiUrl, runID, stage, filter, search, cursor, key]);

  const available = useMemo(() => orderedFields(page?.fields ?? []), [page?.fields]);
  const proseFields = useMemo(() => textFields(available, page?.records ?? []), [available, page?.records]);
  // Numeric or metadata-only datasets remain a table. Language-shaped data
  // starts in the reader so the first screen is useful without configuration.
  const presentation = proseFields.length ? surface : "table";
  const fields = (chosen ?? available.slice(0, 8)).filter(field => available.includes(field));
  const effectiveView = phase === "before" ? "before" : view;
  const rows = useMemo(() => visibleRows(page?.records ?? [], effectiveView), [page?.records, effectiveView]);
  const groups = useMemo(() => groupRows(rows, group, effectiveView), [rows, group, effectiveView]);
  useEffect(() => {
    if (!page || stage === "raw" || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    // Show the actual predecessor values, then reveal the actual output. The
    // diff remains readable after motion stops; nothing is erased from history.
    const start = setTimeout(() => setPhase("before"), 0);
    const finish = setTimeout(() => setPhase("settled"), 650);
    return () => { clearTimeout(start); clearTimeout(finish); };
  }, [page, replay, stage]);

  function changeFilter(value: string) { setFilter(value); setCursors([""]); setPageIndex(0); setCell(null); }
  function movePage(index: number) { setPageIndex(index); setCell(null); scroll.current?.scrollTo({ top: 0 }); }

  const table = <div className="transformation-table" data-phase={phase}>
    <div className="sheet-toolbar">
      <div className="sheet-surface-switch" role="group" aria-label="Data presentation">
        <button type="button" aria-pressed={presentation === "reading"} disabled={!proseFields.length} onClick={() => setSurface("reading")}>Reading</button>
        <button type="button" aria-pressed={presentation === "table"} onClick={() => setSurface("table")}>Table</button>
      </div>
      <div className="sheet-view" role="group" aria-label="Boundary view">
        {(stage === "raw" ? ["after"] : ["changes", "before", "after"]).map(mode => <button key={mode} type="button" aria-pressed={view === mode} onClick={() => { setView(mode as View); setPhase("settled"); setCell(null); }}>{mode === "after" ? stage === "raw" ? "Data" : "After" : mode === "before" ? "Before" : "Changes"}</button>)}
      </div>
      <span className="sheet-boundary">{page?.previous_stage_id ? <>{labels[page.previous_stage_id]} <ArrowRightIcon size={13} /></> : null}{labels[stage]}</span>
      <div className="sheet-tools">
        <label className="sheet-search"><MagnifyingGlassIcon size={15} /><input aria-label="Search all records" maxLength={200} placeholder="Find in all records" value={query} onChange={event => setQuery(event.target.value)} /></label>
        <Popover.Root><Popover.Trigger><button type="button" className="sheet-tool" aria-label="Choose columns"><ColumnsIcon size={16} /><span>Columns</span></button></Popover.Trigger><Popover.Content className="sheet-column-picker" width="280px">
          <strong>Visible columns</strong><input aria-label="Find a column" placeholder="Find a field…" value={columnQuery} onChange={event => setColumnQuery(event.target.value)} />
          <div className="sheet-column-actions"><button type="button" onClick={() => setChosen(available)}>Show all</button>{stage !== "raw" ? <button type="button" onClick={() => setChosen(available.filter(field => page?.records.some(row => cellChange(row, field).state !== "same")))}>Changed fields</button> : null}<button type="button" onClick={() => setChosen(null)}>Reset</button></div>
          <div className="sheet-column-options">{available.filter(field => field.toLowerCase().includes(columnQuery.toLowerCase())).map(field => <label key={field}><input type="checkbox" checked={fields.includes(field)} onChange={event => setChosen(event.target.checked ? [...fields, field] : fields.filter(item => item !== field))} />{field}</label>)}</div>
        </Popover.Content></Popover.Root>
        <button className="sheet-tool" type="button" aria-pressed={wrap} onClick={() => setWrap(!wrap)}>Wrap</button>
        {stage !== "raw" ? <button className="sheet-tool" type="button" disabled={!page || loading} onClick={() => { setView("changes"); setReplay(replay + 1); }}><PlayIcon size={14} />Replay change</button> : null}
        <button className="sheet-tool" type="button" aria-label={expanded ? "Exit expanded table" : "Expand table"} onClick={() => setExpanded(!expanded)}>{expanded ? <XIcon size={16} /> : <ArrowsOutSimpleIcon size={16} />}</button>
      </div>
    </div>
    <div className="sheet-filterbar">
      <div className="sheet-filters" role="group" aria-label="Filter records">
        {[["all", "All rows"], ...(stage === "raw" ? [] : [["changed", "Modified"], ["removed", "Removed"], ["added", "Added"], ["result", "Output rows"]])].map(([value, label]) => <button key={value} type="button" aria-pressed={filter === value} onClick={() => changeFilter(value)}>{label}<span>{page ? (value === "all" ? page.rows : page.counts[value] ?? 0).toLocaleString() : "–"}</span></button>)}
      </div>
      <label className="sheet-group">Group this page<select aria-label="Group this page" value={group} onChange={event => setGroup(event.target.value)}><option value="">None</option>{available.map(field => <option key={field} value={field}>{field}</option>)}</select></label>
    </div>
    {!apiUrl ? <div className="sheet-empty"><TableIcon size={24} /><strong>Connect the API to inspect your data.</strong><span>No sample rows are substituted for your snapshot.</span></div> : loading ? <div className="sheet-empty" role="status"><i className="sheet-loading" /><strong>Opening the {labels[stage]} boundary…</strong><span>The first visit indexes this boundary. Later pages reuse it.</span></div> : error ? <div className="sheet-empty" role="alert"><strong>{error}</strong><button className="sheet-tool" type="button" onClick={() => setRetry(retry + 1)}>Retry</button></div> : presentation === "reading" ? <>
      <TextReader rows={rows} fields={proseFields} view={effectiveView} onRecord={setRecord} />
    </> : <>
      <div className="sheet-scroll" ref={scroll} tabIndex={0} aria-label="Scrollable data table" data-wrap={wrap}>
        <table className="data-sheet" aria-label={`${labels[stage]} data table`}><colgroup><col style={{ width: 48 }} /><col style={{ width: 136 }} />{fields.map(field => <col key={field} style={{ width: /content|message|text|body|signal|title/.test(field) ? 300 : 180 }} />)}</colgroup>
          <thead><tr><th className="sheet-number" scope="col">#</th><th className="sheet-state" scope="col">Record / change</th>{fields.map(field => <th key={field} scope="col"><span>{field}</span></th>)}</tr></thead>
          <tbody>{groups.map(({ label, rows }) => <TableGroup key={label} label={label} rows={rows} fields={fields} view={effectiveView} onCell={(row, field) => setCell({ row, field })} onRecord={setRecord} />)}</tbody>
        </table>
        {!rows.length ? <div className="sheet-empty"><strong>{page?.records.length ? `These rows have no ${effectiveView === "before" ? "Before" : "After"} values.` : page?.next_cursor ? "No matching rows in this segment." : "No rows in this view."}</strong><span>{page?.records.length ? "Switch to Changes to see the original records and their outcome." : page?.next_cursor ? "Continue to the next page, or change your filter." : "Try All rows or a different view. The source snapshot is unchanged."}</span></div> : null}
      </div>
      {cell ? <div className="sheet-cell-detail" aria-label="Selected cell details"><header><strong>{cell.field}</strong><code>{cell.row.record_id}</code><button type="button" className="icon-button" aria-label="Close cell details" onClick={() => setCell(null)}><XIcon size={16} /></button></header><div>{cell.row.before ? <section><span>Before</span><pre>{fullValue(recordBody(cell.row.before)?.[cell.field])}</pre></section> : null}<section><span>After</span><pre>{fullValue(recordBody(cell.row.after)?.[cell.field])}</pre></section></div></div> : null}
    </>}
    <footer className="sheet-footer"><span>{page ? <>{rows.length} visible · page {pageIndex + 1} · {fields.length} of {available.length} fields{group ? " · groups are page-local" : ""}</> : "Up to 100 records per page"}</span><div><span className="sheet-legend"><i data-kind="added" />Added <i data-kind="changed" />Modified <i data-kind="removed" />Removed · ∅ missing</span><button type="button" disabled={loading || pageIndex === 0} onClick={() => movePage(pageIndex - 1)}>Previous</button><button type="button" disabled={loading || !page?.next_cursor} onClick={() => { if (page?.next_cursor) { setCursors([...cursors.slice(0, pageIndex + 1), page.next_cursor]); movePage(pageIndex + 1); } }}>Next</button></div></footer>
    <span className="sr-only" role="status">{phase === "before" ? "Showing values before the transformation" : `${labels[stage]} ${effectiveView} view. ${rows.length} rows visible.`}</span>
  </div>;

  return <>
    {expanded ? null : table}
    <Dialog.Root open={expanded} onOpenChange={setExpanded}><Dialog.Content className="verity-dialog sheet-expanded" maxWidth="calc(100vw - 32px)"><Dialog.Title className="sr-only">{labels[stage]} expanded data table</Dialog.Title><Dialog.Description className="sr-only">Browse and compare the current pipeline boundary.</Dialog.Description>{expanded ? table : null}</Dialog.Content></Dialog.Root>
    <Dialog.Root open={Boolean(record)} onOpenChange={open => { if (!open) setRecord(null); }}><Dialog.Content className="verity-dialog sheet-record-dialog" maxWidth="1040px"><div className="dialog-heading"><div><Dialog.Title>{record?.record_id}</Dialog.Title><Dialog.Description>Complete values at this boundary, including source metadata.</Dialog.Description></div><Dialog.Close><button type="button" className="icon-button" aria-label="Close"><XIcon /></button></Dialog.Close></div>{record ? <div className="sheet-record-detail"><p><strong>{changes[record.change]}</strong> · {record.reason || "Compared against the previous stage."}</p><div className="sheet-full-records">{record.before ? <section><h3>Before</h3><pre>{JSON.stringify(record.before, null, 2)}</pre></section> : null}<section><h3>{record.before ? "After" : "Input record"}</h3><pre>{record.after ? JSON.stringify(record.after, null, 2) : record.change === "elsewhere" ? "Routed outside this branch. The record was not deleted." : "Removed at this stage. The original record remains in the input snapshot."}</pre></section></div></div> : null}</Dialog.Content></Dialog.Root>
  </>;
}

function textOf(value: unknown) {
  if (typeof value === "string") return value;
  if (value === undefined || value === null) return "";
  return stableValue(value);
}

function TextReader({ rows, fields, view, onRecord }: { rows: TableRecord[]; fields: string[]; view: View; onRecord: (row: TableRecord) => void }) {
  return <div className="text-reader" aria-label="Readable record view">
    <div className="text-reader-intro"><div><span className="text-reader-kicker">Readable evidence</span><strong>What the source is saying</strong></div><span>Changes are marked in the sentence, not hidden in a column.</span></div>
    <div className="text-record-list">
      {rows.map((row, index) => {
        const before = recordBody(row.before), after = recordBody(row.after);
        const current = view === "before" ? before : after ?? before;
        const titleField = fields.find((field) => ["title", "subject"].includes(field.toLowerCase()));
        const title = titleField ? textOf(current?.[titleField]) : "";
        const changedMeta = fields.filter((field) => cellChange(row, field).state !== "same");
        const metadataChanges = ["kind", "type", "status", "state", "actor", "occurred_at", "timestamp", "owner", "source"].filter((field) => cellChange(row, field).state !== "same");
        const bodyFields = fields.filter((field) => field !== titleField && textOf(current?.[field]).trim());
        return <article className="text-record" key={`${row.record_id}-${row.ordinal}`} data-record-id={row.record_id} data-change={row.change}>
          <header className="text-record-head">
            <div className="text-record-context"><span className="text-record-number">{String(index + 1).padStart(2, "0")}</span><code>{row.record_id || "Unnamed record"}</code><span className="text-change" data-kind={row.change}>{changes[row.change]}</span></div>
            <button className="text-inspect" type="button" onClick={() => onRecord(row)}>Inspect record <ArrowRightIcon size={14} /></button>
          </header>
          {title ? <h3 className="text-record-title">{renderTextField(row, titleField!, view)}</h3> : null}
          <div className="text-record-copy">
            {bodyFields.map((field) => <section className="text-field" key={field}><span className="text-field-label">{field.replaceAll("_", " ")}</span><p>{renderTextField(row, field, view)}</p></section>)}
            {!bodyFields.length && !title ? <p className="text-no-copy">No readable text at this boundary. Inspect the record for structured fields.</p> : null}
          </div>
          <footer className="text-record-foot"><span>{textOf(current?.occurred_at ?? current?.timestamp) || "Time not provided"}</span>{metadataChanges.length ? <span>{metadataChanges.length} context {metadataChanges.length === 1 ? "field" : "fields"} changed</span> : null}{changedMeta.length ? <details><summary>Why it changed</summary><span>{changedMeta.join(" · ")}</span>{row.reason ? <span> · {row.reason}</span> : null}</details> : row.reason ? <span>{row.reason}</span> : null}</footer>
        </article>;
      })}
      {!rows.length ? <div className="text-reader-empty">No readable records in this view. Switch to Changes or Table to inspect the boundary.</div> : null}
    </div>
  </div>;
}

function renderTextField(row: TableRecord, field: string, view: View) {
  const diff = cellChange(row, field);
  const left = textOf(diff.left), right = textOf(diff.right);
  if (view === "before") return left || <span className="text-missing">∅ Missing</span>;
  if (view === "after") return right || <span className="text-missing">∅ Missing</span>;
  if (diff.state === "changed" && typeof diff.left === "string" && typeof diff.right === "string") return <>{diffText(left, right).map((part, index) => part.kind === "same" ? <span key={index}>{part.text}</span> : <mark key={index} data-diff={part.kind}>{part.text}</mark>)}</>;
  if (diff.state === "removed") return <mark data-diff="removed">{left}</mark>;
  if (diff.state === "added") return <mark data-diff="added">{right}</mark>;
  return right || left || <span className="text-missing">∅ Missing</span>;
}

function TableGroup({ label, rows, fields, view, onCell, onRecord }: { label: string; rows: TableRecord[]; fields: string[]; view: View; onCell: (row: TableRecord, field: string) => void; onRecord: (row: TableRecord) => void }) {
  return <>{label ? <tr className="sheet-group-row"><th colSpan={fields.length + 2}>{preview(label)} <span>{rows.length} rows on this page</span></th></tr> : null}{rows.map(row => <tr key={row.ordinal} data-record-id={row.record_id} data-change={row.change}>
    <td className="sheet-number">{row.ordinal}</td><td className="sheet-state"><button type="button" className="sheet-record-button" title={row.reason || row.record_id} aria-label={`Inspect ${row.record_id} row ${row.ordinal}`} onClick={() => onRecord(row)}><code>{row.record_id || "Unnamed record"}</code><span data-kind={row.change}>{changes[row.change]}</span></button></td>
    {fields.map(field => { const diff = cellChange(row, field); return <td key={field} data-cell-change={view === "changes" ? diff.state : undefined}><button className="sheet-cell" type="button" aria-label={`${field}, row ${row.ordinal}`} onClick={() => onCell(row, field)} onKeyDown={event => {
      const horizontal = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
      const vertical = event.key === "ArrowDown" ? 1 : event.key === "ArrowUp" ? -1 : 0;
      if (!horizontal && !vertical) return;
      const td = event.currentTarget.closest("td"); const tr = td?.parentElement;
      const nextRow = vertical > 0 ? tr?.nextElementSibling : tr?.previousElementSibling;
      const nextCell = horizontal > 0 ? td?.nextElementSibling : horizontal < 0 ? td?.previousElementSibling : nextRow?.children[Array.from(tr?.children ?? []).indexOf(td!)];
      const button = nextCell?.querySelector<HTMLButtonElement>("button"); if (button) { event.preventDefault(); button.focus(); }
    }}>
      {view === "changes" && diff.state === "changed" ? <><del>{preview(diff.left)}</del><ins>{preview(diff.right)}</ins></> : view === "changes" && diff.state === "removed" ? <del>{preview(diff.left)}</del> : view === "changes" && diff.state === "added" ? <ins>{preview(diff.right)}</ins> : <span data-missing={!(view === "before" ? diff.had : diff.has)}>{preview(view === "before" ? diff.left : diff.right)}</span>}
    </button></td>; })}
  </tr>)}</>;
}
