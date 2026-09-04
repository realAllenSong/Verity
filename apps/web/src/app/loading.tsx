export default function Loading() {
  return (
    <main className="loading-shell" aria-label="Loading workspace">
      <div className="loading-rail" />
      <div className="loading-canvas">
        <div className="loading-line loading-line-wide" />
        <div className="loading-flow" />
        <div className="loading-table" />
      </div>
      <div className="loading-inspector" />
    </main>
  );
}
