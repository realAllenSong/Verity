"use client";

import { useEffect, useState } from "react";
import { Dialog } from "@radix-ui/themes";
import { XIcon } from "@phosphor-icons/react";

type Page = { rows: Record<string, unknown>[]; next_cursor?: string };

export function RecordBrowser({ stage, apiUrl }: { stage: string; apiUrl: string }) {
  const [open, setOpen] = useState(false);
  const [cursors, setCursors] = useState([""]);
  const [index, setIndex] = useState(0);
  const [result, setResult] = useState<{ key: string; page?: Page; error?: string }>();
  const [retry, setRetry] = useState(0);
  const cursor = cursors[index];
  const key = `${stage}:${cursor}:${retry}`;
  const loading = result?.key !== key;
  const page = !loading ? result?.page : undefined;
  useEffect(() => {
    if (!open) return;
    const abort = new AbortController();
    void fetch(`${apiUrl}/api/v1/stages/${stage}/records?limit=25&cursor=${encodeURIComponent(cursor)}`, { cache: "no-store", signal: abort.signal })
      .then(async (response) => { if (!response.ok) throw new Error("This page could not be read."); return response.json() as Promise<Page>; })
      .then((page) => setResult({ key, page }))
      .catch((error: Error) => { if (!abort.signal.aborted) setResult({ key, error: error.message }); });
    return () => abort.abort();
  }, [apiUrl, stage, cursor, key, open]);

  return <Dialog.Root open={open} onOpenChange={setOpen}>
    <Dialog.Trigger><button className="browse-trigger" type="button">Browse all records</button></Dialog.Trigger>
    <Dialog.Content className="verity-dialog" maxWidth="860px">
      <div className="dialog-heading"><div><Dialog.Title>Browse {stage} records</Dialog.Title><Dialog.Description>Full stage data, 25 records at a time. Expand a record to read every field.</Dialog.Description></div><Dialog.Close><button className="icon-button" type="button" aria-label="Close"><XIcon /></button></Dialog.Close></div>
      <div className="record-browser">
        {loading ? <p role="status">Reading records...</p> : result?.error ? <p role="alert">{result.error} <button type="button" onClick={() => setRetry(retry + 1)}>Retry</button></p> : null}
        {page?.rows.map((record, offset) => <details className="browse-record" key={`${cursor}-${offset}`}>
          <summary><code>{String(record.event_id ?? record.record_id ?? `Record ${index * 25 + offset + 1}`)}</code></summary>
          <dl>{Object.entries((record.payload && typeof record.payload === "object" ? record.payload : record) as Record<string, unknown>).map(([field, value]) => <div key={field}><dt>{field}</dt><dd>{typeof value === "object" ? JSON.stringify(value, null, 2) : String(value)}</dd></div>)}</dl>
        </details>)}
        {page?.rows.length === 0 ? <p>No records in this stage.</p> : null}
      </div>
      <footer className="browse-pagination"><button type="button" disabled={loading || index === 0} onClick={() => setIndex(index - 1)}>Previous</button><span>Page {index + 1}</span><button type="button" disabled={loading || !page?.next_cursor} onClick={() => { if (!page?.next_cursor) return; setCursors([...cursors.slice(0, index + 1), page.next_cursor]); setIndex(index + 1); }}>Next</button></footer>
    </Dialog.Content>
  </Dialog.Root>;
}
