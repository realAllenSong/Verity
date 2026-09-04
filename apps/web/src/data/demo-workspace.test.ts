import { describe, expect, it } from "vitest";
import workspace from "./demo-workspace.json";

describe("demo workspace contract", () => {
  it("shows the deterministic white-box funnel", () => {
    expect(Object.fromEntries(workspace.stages.map((stage) => [stage.id, stage.count]))).toEqual({
      raw: 3842,
      normalize: 3611,
      privacy: 3276,
      quality: 2914,
      signals: 1086,
      review: 42,
      curated: 1044,
    });
  });

  it("models independently traceable batches with generic records", () => {
    expect(workspace.batches).toHaveLength(6);
    expect(workspace.dataset.record_count).toBe(3842);
    expect(workspace.records.every((record) => record.batch_id.startsWith("batch_"))).toBe(true);
    expect(workspace.records.every((record) => Object.keys(record.before_fields).length > 0)).toBe(true);
    expect(new Set(workspace.records.map((record) => record.signal_type)).size).toBeGreaterThan(5);
  });
});
