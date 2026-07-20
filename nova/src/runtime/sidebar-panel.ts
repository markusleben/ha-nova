import { existsSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import type { createSupervisorClient } from "../ha/supervisor-client.js";
import type { RelayLogger } from "../http/server.js";

// One-time App default: enable the NOVA sidebar entry (`ingress_panel`) on the
// first App-mode boot, so "open NOVA in the sidebar" is true out of the box.
// The marker makes this once-ever — an owner who later hides the panel is never
// overridden. On Supervisor failure no marker is written, so the default is
// retried on the next start.

const MARKER_FILE = "sidebar_default_applied";

export interface SidebarPanelDeps {
  supervisor: ReturnType<typeof createSupervisorClient>;
  dataDir: string;
  logger: RelayLogger;
}

export async function ensureSidebarPanel(deps: SidebarPanelDeps): Promise<void> {
  const marker = join(deps.dataDir, MARKER_FILE);
  if (existsSync(marker)) {
    return;
  }
  try {
    const info = await deps.supervisor.getSelfInfo();
    if (!info.ingressPanel) {
      await deps.supervisor.setIngressPanel(true);
      deps.logger.info?.("Enabled the NOVA sidebar entry (ingress_panel) as the App default");
    }
    writeFileSync(marker, "");
  } catch (error) {
    deps.logger.warn("Could not apply the sidebar default; retrying on next start", {
      error: String((error as Error).message),
    });
  }
}
