"use client";

import { useRef, useState } from "react";
import { ArrowUpRightIcon, FileArrowUpIcon } from "@phosphor-icons/react";
import type { ActiveImport } from "./workspace-machine";
import { stateLabel } from "./workspace-machine";

const accepted = ".csv,.tsv,.json,.jsonl,.ndjson,.parquet,.csv.gz,.tsv.gz,.json.gz,.jsonl.gz";

export function ImportDropzone({ active, disabled, onFiles }: {
  active?: ActiveImport;
  disabled?: boolean;
  onFiles: (files: File[]) => void;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const progress = active && active.bytes > 0 ? Math.min(100, Math.round(active.uploaded / active.bytes * 100)) : 0;

  function receive(list: FileList | null) {
    const files = Array.from(list ?? []);
    if (files.length) onFiles(files);
  }

  return (
    <div
      className="import-surface"
      data-active={dragging}
      data-ready="true"
      data-state={active?.state ?? "idle"}
      role="button"
      tabIndex={disabled ? -1 : 0}
      aria-disabled={disabled}
      onClick={() => { if (!disabled) input.current?.click(); }}
      onKeyDown={(event) => {
        if (!disabled && (event.key === "Enter" || event.key === " ")) {
          event.preventDefault();
          input.current?.click();
        }
      }}
      onDragEnter={(event) => { event.preventDefault(); setDragging(true); }}
      onDragOver={(event) => { event.preventDefault(); event.dataTransfer.dropEffect = "copy"; setDragging(true); }}
      onDragLeave={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDragging(false); }}
      onDrop={(event) => { event.preventDefault(); setDragging(false); if (!disabled) receive(event.dataTransfer.files); }}
      aria-label="Drop data or choose files"
    >
      <input
        ref={input}
        tabIndex={-1}
        type="file"
        multiple
        accept={accepted}
        onChange={(event) => { receive(event.target.files); event.currentTarget.value = ""; }}
      />
      <span className="import-icon"><FileArrowUpIcon size={22} weight="duotone" /></span>
      <span className="import-copy">
        <strong>{active ? active.filename : "Drop your data here"}</strong>
        <small>{active?.state === "ready" ? "Drop another file to start a new run" : active ? stateLabel(active) : "CSV, JSON, JSONL, TSV, Parquet, or gzip"}</small>
      </span>
      {!disabled ? <span className="import-action">Choose files<ArrowUpRightIcon size={16} /></span> : null}
      {active && active.state === "uploading" ? <span className="import-progress" aria-label={`${progress}% uploaded`}><i style={{ transform: `scaleX(${progress / 100})` }} /></span> : null}
    </div>
  );
}
