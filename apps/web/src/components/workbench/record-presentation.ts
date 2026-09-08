const contentFields = ["content", "message", "body", "text", "title", "subject", "name"];

export function recordBody(record?: Record<string, unknown>) {
  if (!record) return undefined;
  const nested = record.payload;
  return nested && typeof nested === "object" && !Array.isArray(nested)
    ? nested as Record<string, unknown>
    : record;
}

/** Presentation only: all remaining fields stay available in the disclosure. */
export function presentationFields(before?: Record<string, unknown>, after?: Record<string, unknown>, changed: string[] = []) {
  const keys = [...new Set([...Object.keys(recordBody(before) ?? {}), ...Object.keys(recordBody(after) ?? {})])];
  const relevant = [...new Set([...contentFields.filter((key) => keys.includes(key)), ...changed.filter((key) => keys.includes(key))])];
  const primary = (relevant.length ? relevant : keys).slice(0, 6);
  const secondary = keys.filter((key) => !primary.includes(key));
  return { primary, secondary, additionalChanges: secondary.filter((key) => changed.includes(key)).length };
}
