import workspaceFixture from "@/data/demo-workspace.json";
import { Workbench } from "@/components/workbench/workbench";
import type { WorkspaceData } from "@/lib/contracts";

async function loadWorkspace(): Promise<WorkspaceData> {
  const apiUrl = process.env.VERITY_API_URL;
  if (!apiUrl) return workspaceFixture as WorkspaceData;
  try {
    const response = await fetch(`${apiUrl}/api/v1/workspace`, { cache: "no-store" });
    if (response.ok) return (await response.json()) as WorkspaceData;
  } catch {
    // The checked-in synthetic fixture keeps the workbench usable without the API.
  }
  return workspaceFixture as WorkspaceData;
}

export default async function Home() {
  return <Workbench initialWorkspace={await loadWorkspace()} />;
}
