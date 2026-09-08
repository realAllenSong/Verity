import { describe, expect, it } from "vitest";
import { presentationFields, recordBody } from "./record-presentation";

describe("progressive record presentation", () => {
  it("prioritizes readable content and changed fields without losing metadata", () => {
    const before = { payload: { id: "a", actor: "fixture", timestamp: "2026-09-08", content: "hello", kind: "raw", extra: null } };
    const after = { payload: { ...before.payload, content: "Hello", kind: "clean" } };
    const fields = presentationFields(before, after, ["content", "kind"]);
    expect(fields.primary).toEqual(["content", "kind"]);
    expect(fields.secondary).toEqual(["id", "actor", "timestamp", "extra"]);
    expect([...fields.primary, ...fields.secondary].sort()).toEqual(Object.keys(before.payload).sort());
  });

  it("supports arbitrary tabular schemas, not only agent data", () => {
    const row = { temperature: 32, humidity: 50, valid: true, optional: null };
    expect(recordBody(row)).toEqual(row);
    expect(presentationFields(row, row).primary).toEqual(Object.keys(row));
    expect(recordBody({ payload: [1, 2], name: "array" })).toEqual({ payload: [1, 2], name: "array" });
  });

  it("keeps added and removed fields and flags changes inside a disclosure", () => {
    const before = { a: 1, b: 2, c: 3, d: 4, e: 5, f: 6, g: 7, removed: true };
    const after = { a: 2, b: 3, c: 4, d: 5, e: 6, f: 7, g: 8, added: true };
    const changed = [...new Set([...Object.keys(before), ...Object.keys(after)])];
    const fields = presentationFields(before, after, changed);
    expect(fields.primary).toHaveLength(6);
    expect(fields.secondary).toEqual(["g", "removed", "added"]);
    expect(fields.additionalChanges).toBe(3);
  });

  it("handles filtered records and empty boundaries without inventing fields", () => {
    expect(presentationFields({ content: "removed" }, undefined).primary).toEqual(["content"]);
    expect(presentationFields()).toEqual({ primary: [], secondary: [], additionalChanges: 0 });
  });
});
