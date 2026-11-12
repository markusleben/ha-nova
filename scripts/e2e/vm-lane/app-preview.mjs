// The App promote PR's preview on the throwaway HAOS VM (vm-lane --app-branch).
// Spec: docs/work/2026-11-09-app-preview-channel-spec.md (Appendix: store switch, VMs only).
export const APP_BRANCH_RE = /^chore\/app-(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const SEMVER_RE = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const newer = (a, b) => {
  const [x, y] = [a, b].map((v) => v.split(".").map(Number));
  for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] > y[i];
  return false;
};

// ctx: { repo, app, fetch, sup, host, imageCheck, page, registryHash, check, sleep }
// host runs a root shell command on the HAOS VM; imageCheck runs verify-app-image.sh <version>.
export function appPreview(ctx) {
  const repoSlug = ctx.app.split("_")[0];
  if (!/^[0-9a-f]{8}$/.test(repoSlug)) throw new Error("App slug has no store repository hash");
  const git = (cmd) => ctx.host(`docker exec hassio_supervisor sh -c 'cd /data/apps/git/${repoSlug} && ${cmd}'`);
  const configVersion = async (ref) => {
    const res = await ctx.fetch(`https://raw.githubusercontent.com/${ctx.repo}/${ref}/nova/config.yaml`);
    if (!res.ok) throw new Error(`${ref}/nova/config.yaml: HTTP ${res.status}`);
    const version = (await res.text()).match(/^version:\s*"?([^"\s]+)"?\s*$/m)?.[1];
    if (!SEMVER_RE.test(version ?? "")) throw new Error(`${ref}: nova/config.yaml has no X.Y.Z App version`);
    return version;
  };
  let switched, stable;
  // arch -> { image, digest } from verify-app-image.sh, which checks every build arch.
  const images = (version) => Object.fromEntries([...ctx.imageCheck(version).matchAll(/OK: (\S+):\S+ \(\S+\) (\S+)=(sha256:[0-9a-f]{64})$/gm)].map((m) => [m[2], { image: m[1], digest: m[3] }]));
  const restartSupervisor = async (want) => {
    try { await ctx.sup("/supervisor/restart", "post"); } catch { /* the answer can drop while it restarts */ }
    for (let i = 0; i < 60; i++) {
      await ctx.sleep(5000);
      const store = await ctx.sup(`/store/addons/${ctx.app}`).catch(() => null);
      if (store?.version_latest === want) return true;
    }
    return false;
  };
  return {
    // Switches the store to the promote branch and updates the App in place; true when every row passed.
    async install(branch, registryAtStart) {
      if (!APP_BRANCH_RE.test(branch)) throw new Error("--app-branch must be chore/app-X.Y.Z");
      const preview = await configVersion(branch);
      stable = await configVersion("main");
      const installed = (await ctx.sup(`/addons/${ctx.app}/info`)).version;
      if (!ctx.check("a", "App preview is newer than main's and the installed App", newer(preview, stable) && newer(preview, installed), `${branch}: ${preview}, main: ${stable}, installed: ${installed}`)) return false;
      const all = images(preview);
      const want = all[(await ctx.sup("/info")).arch];
      if (!ctx.check("a", "App preview images (record on the promote PR)", Boolean(want), Object.entries(all).map(([a, i]) => `${a}=${i.digest}`).join(" "))) return false;
      switched = true;
      git(`git fetch -q --depth 1 origin +refs/heads/${branch}:refs/remotes/origin/${branch} && git checkout -q -B ${branch} origin/${branch}`);
      if (!ctx.check("a", "store offers the preview after a Supervisor restart", await restartSupervisor(preview), `${branch} -> ${preview}`)) return false;
      await ctx.sup(`/store/addons/${ctx.app}/update`, "post", { backup: false }).catch(() => null); // the answer can drop while it updates: judged below
      let info = {};
      for (let i = 0; i < 60 && !(info.state === "started" && info.version === preview); i++) { await ctx.sleep(5000); info = await ctx.sup(`/addons/${ctx.app}/info`).catch(() => ({})); }
      if (!ctx.check("a", "App preview installed in place", info.state === "started" && info.version === preview, `${ctx.app} ${info.version ?? "?"} ${info.state ?? "?"}`)) return false;
      // Only the pulled image lists the registry's tag digest under the GHCR name (a local build doesn't); the container must run it.
      const [tagged, running] = ctx.host(`docker image inspect --format '{{.Id}} {{json .RepoDigests}}' ${want.image}:${preview}; docker inspect --format '{{.Image}}' app_${ctx.app}`).trim().split(/\s*\n\s*/);
      const ok = [
        ctx.check("a", "App preview runs the image pulled at the recorded digest", Boolean(running) && tagged.startsWith(`${running} `) && tagged.includes(`${want.image}@${want.digest}`), `${want.image}@${want.digest}`),
        ctx.check("a", "pairing kept (device registry and TLS key unchanged)", ctx.registryHash() === registryAtStart, "paired computers pin the TLS key; lastUsedAtMs and the App's own safety-check record (minted at start) excluded"),
      ];
      const html = await ctx.page();
      ok.push(ctx.check("a", "NOVA page: Home Assistant connected, preview version", html.includes("<h2>Home Assistant</h2><p>Connected.</p>") && html.includes(`Relay ${preview}</footer>`), preview));
      return ok.every(Boolean);
    },
    // Back to stable: a repository repair re-clones main; only a Supervisor start re-reads it. No network before the repair.
    async restoreStore() {
      if (!switched) return true;
      ctx.host(`ha store repair ${repoSlug} --no-progress`);
      const ok = await restartSupervisor(stable);
      const branch = git("git rev-parse --abbrev-ref HEAD");
      return ctx.check("c", "store back on main", ok && /^main$/m.test(branch), ok ? `offers ${stable}` : `STORE NOT CONFIRMED ON MAIN (offers no ${stable}); run ha store repair ${repoSlug} and restart the Supervisor`);
    },
  };
}
