import React from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ContentReader } from "./content-reader";
import type { TableRecord } from "./table-model";

afterEach(cleanup);
const blocks = [
  { id: "one", role: "user", text: "Keep the public contract unchanged.", source_path: "messages[0].content" },
  { id: "two", role: "assistant", text: "I will add another argument.", source_path: "messages[1].content" },
  { id: "three", role: "user", interaction: "correction", reply_to: "two", text: "Stop. Use the existing RetryPolicy.", source_path: "messages[2].content" },
];
const row: TableRecord = { ordinal: 1, record_id: "sample", change: "changed", outcome: "kept", before: { messages: [] }, after: { content: "actual text" }, before_document: { source: "Codex", blocks }, after_document: { source: "Codex", blocks, synthetic: true } };

describe("source content reader", () => {
  it("reads actual roles and correction text, not timestamps or an invented summary", () => {
    render(<ContentReader rows={[row]} view="changes" onRecord={vi.fn()} />);
    expect(screen.getByText("User prompt")).toBeVisible();
    expect(screen.getByText("Agent reply")).toBeVisible();
    expect(screen.getByText("User correction")).toBeVisible();
    expect(screen.getByText("Stop. Use the existing RetryPolicy.")).toBeVisible();
    expect(screen.getByText("Synthetic sample")).toBeVisible();
    expect(screen.queryByText("Time not provided")).toBeNull();
    fireEvent.click(screen.getByText("Source field · linked reply"));
    expect(screen.getByText("Reply to two")).toBeVisible();
  });
  it("aligns blocks across normalization without pretending every message was added", () => {
    const { container } = render(<ContentReader rows={[row]} view="changes" onRecord={vi.fn()} />);
    expect(container.querySelectorAll("mark")).toHaveLength(0);
    expect(screen.getByText(/Content preserved/)).toBeVisible();
  });
  it("shows true changed text and removed records", () => {
    const changed = { ...row, after_document: { blocks: [{ ...blocks[0], text: "Keep the existing contract unchanged." }] } };
    const { container } = render(<ContentReader rows={[changed]} view="changes" onRecord={vi.fn()} />);
    expect(container.querySelector('mark[data-diff="added"]')).toHaveTextContent("existing");
    expect(container.querySelectorAll('[data-change="removed"]')).toHaveLength(2);
  });
  it("preserves longer conversations and tool output behind inline expansion", () => {
    const longer = { ...row, after_document: { blocks: [...blocks, { ...blocks[1], id: "four" }, { ...blocks[1], id: "five", text: "Final local test result." }, { id: "tool", role: "tool_result", text: "PASS", source_path: "tool.output" }] } };
    render(<ContentReader rows={[longer]} view="after" onRecord={vi.fn()} />);
    expect(screen.queryByText("Final local test result.")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Show 1 more message" }));
    expect(screen.getByText("Final local test result.")).toBeVisible();
    fireEvent.click(screen.getByText("Tool result"));
    expect(screen.getByText("PASS")).toBeVisible();
  });
});
