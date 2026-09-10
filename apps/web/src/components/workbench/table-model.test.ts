import { describe, expect, it } from "vitest";
import { cellChange, diffText, groupRows, orderedFields, stableValue, textFields, visibleRows, type TableRecord } from "./table-model";

const row: TableRecord = { record_id: "one", ordinal: 1, change: "changed", outcome: "kept", before: { payload: { title: "before", removed: null, typed: 0, nested: { a: 1, b: [2] } } }, after: { title: "after", added: false, typed: "0", nested: { b: [2], a: 1 } } };
describe("spreadsheet transformation semantics", () => {
  it("unwraps input payloads and distinguishes added/removed/modified cells", () => {
    expect(cellChange(row, "title").state).toBe("changed");
    expect(cellChange(row, "removed")).toMatchObject({ had: true, has: false, left: null, state: "removed" });
    expect(cellChange(row, "added")).toMatchObject({ right: false, state: "added" });
    expect(cellChange(row, "typed").state).toBe("changed");
    expect(cellChange(row, "nested").state).toBe("same");
    expect(cellChange({ ...row, before: { values: [1] }, after: { values: ["1"] } }, "values").state).toBe("changed");
  });
  it("keeps removals in Changes, never in the output", () => {
    const removed = { ...row, ordinal: 2, after: undefined, change: "removed" as const };
    const added = { ...row, ordinal: 3, before: undefined, change: "added" as const };
    expect(visibleRows([row, removed, added], "changes")).toHaveLength(3);
    expect(visibleRows([row, removed, added], "before")).toEqual([row, removed]);
    expect(visibleRows([row, removed, added], "after")).toEqual([row, added]);
  });
  it("keeps arbitrary schemas and groups only the provided page", () => {
    expect(orderedFields(["metadata", "humidity", "temperature", "content"])).toEqual(["content", "humidity", "temperature", "metadata"]);
    expect(groupRows([row], "added", "after")[0]).toMatchObject({ label: "false", rows: [row] });
    expect(stableValue(null)).toBe("null");
    expect(stableValue(undefined)).toBe("∅");
    expect(stableValue({ temperature: 12 })).toContain("12");
  });
  it("selects prose fields for the reader without assuming a source schema", () => {
    expect(textFields(["event_id", "value", "prompt", "owner"], [{ ...row, before: { prompt: "A long developer instruction that should be readable." }, after: { prompt: "A long developer instruction that should be readable." } }])).toEqual(["prompt"]);
    expect(textFields(["temperature", "humidity"], [{ ...row, before: { temperature: 12 }, after: { temperature: 13 } }])).toEqual([]);
  });
  it("marks changed words while preserving whitespace and punctuation", () => {
    expect(diffText("Use the approved API client.", "Use the approved local API client.")).toEqual([
      { kind: "same", text: "Use the approved " },
      { kind: "added", text: "local " },
      { kind: "same", text: "API client." },
    ]);
    expect(diffText("same", "same")).toEqual([{ kind: "same", text: "same" }]);
  });
});
