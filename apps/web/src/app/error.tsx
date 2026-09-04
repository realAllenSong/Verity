"use client";

export default function ErrorPage({ reset }: { reset: () => void }) {
  return (
    <main className="error-shell">
      <p className="error-kicker">Workspace unavailable</p>
      <h1>The run metadata could not be loaded.</h1>
      <p>Your local artifacts are untouched. Retry the read or inspect the API health endpoint.</p>
      <button type="button" onClick={reset}>Retry</button>
    </main>
  );
}
