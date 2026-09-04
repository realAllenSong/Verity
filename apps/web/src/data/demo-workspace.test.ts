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

  it("contains believable examples from every synthetic connector", () => {
    expect(new Set(workspace.records.map((record) => record.source))).toEqual(
      new Set(workspace.sources.map((source) => source.id)),
    );
    expect(workspace.records.find((record) => record.source === "github")?.signal_type).toBe(
      "verification_gap",
    );
    expect(workspace.records.find((record) => record.source === "codex")?.signal_type).toBe(
      "correction",
    );
  });
});
