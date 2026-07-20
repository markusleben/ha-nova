import { existsSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import type { createSupervisorClient } from "../ha/supervisor-client.js";
import type { RelayLogger } from "../http/server.js";

// One-time App default: enable the NOVA sidebar entry (`ingress_panel`) on the
// first App-mode boot, so "open NOVA in the sidebar" is true out of the box.
// The marker makes this once-ever — an owner who later hides the panel is never
// overridden. On Supervisor failure no marker is written, so the default is
// retried on the next start.
//
// Deliberate (maintainer decision 2026-07-20): the one-time enable also applies
// to UPGRADES from pre-marker versions. `ingress_panel: false` cannot
// distinguish "never enabled" from "deliberately hidden", and the 0.19
// migration sends every upgrader to the NOVA page (re-pair, revoke legacy) —
// without the sidebar entry they hit exactly the wall this removes. The cost is
// capped: an owner who had hidden the panel sees it restored at most once,
// ever; hiding it again is permanent. Announced in the 0.19.0 release notes.

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
