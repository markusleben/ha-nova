// 0.31 RC rows that need a computer paired with the App preview (MAR-144), run
// by run.mjs in the Win11 lane's PowerShell 7 user, after its core rows. Each
// row fails on the defect it guards and passes on the fix; nothing here is
// sent to Home Assistant except the language switch row 1 undoes itself.
// Spec: docs/work/2026-10-26-vm-lane-spec.md → "0.31 rows".
const ACCEPT = { type: "marketplace/warning/accept" };
const INSTALL = { type: "marketplace/repository/install", repository: "123456" };
const WHY = "raw.why.marketplace_warning";
const PHONE_WHY_LIMIT = 300; // runes the phone shows of a reason (cli/confirm_phone_text.go)
const PLAN_ID = /^[A-Za-z0-9_-]{6,}$/;
const REF = /^[0-9a-f]{64}$/;

// The JSON envelope in a verb's output (stdout first; stderr may follow it).
export function envelope(out) {
  const start = out.indexOf("{");
  for (let end = out.lastIndexOf("}"); start >= 0 && end > start; end = out.lastIndexOf("}", end - 1)) {
    try { return JSON.parse(out.slice(start, end + 1)); } catch { /* a shorter slice */ }
  }
  return {};
}

const atLeast = (version, floor) => {
  const [a, b] = [version, floor].map((v) => String(v ?? "").split(".").map((n) => parseInt(n, 10) || 0));
  for (let i = 0; i < 3; i++) if ((a[i] ?? 0) !== (b[i] ?? 0)) return (a[i] ?? 0) > (b[i] ?? 0);
  return true;
};

// Row 1 (#1132): what each plan must show. plans: { en, de, bundled } as { code, out }; why: { en, de }.
export function judgeMarketplace(plans, why) {
  const bad = [];
  for (const lang of ["en", "de"]) {
    const { code, out } = plans[lang];
    const text = why[lang] ?? "";
    if (code !== 3 || out.tier !== "typed" || !PLAN_ID.test(out.plan?.id ?? "")) bad.push(lang + ": exit " + code + ", tier " + out.tier + ", " + (out.error?.code ?? "") + " " + (out.error?.message ?? "no plan"));
    else if ((out.result?.items ?? []).length !== 1) bad.push(lang + ": " + (out.result?.items ?? []).length + " items, not the acceptance alone");
    else if (!(out.reasons ?? []).some((r) => r.detail === why.en)) bad.push(lang + ": the plan's reason is not the full English text");
    else if (!text || !(out.say ?? "").includes(text)) bad.push(lang + ": say lacks the full " + lang + " reason");
    if ([...text].length + 1 > PHONE_WHY_LIMIT) bad.push(lang + ": the reason has " + [...text].length + " runes; the phone cuts it at " + PHONE_WHY_LIMIT);
  }
  const { code, out } = plans.bundled;
  if (code !== 4 || out.plan || !/plan of its own/.test(out.error?.message ?? "")) bad.push("with an install: exit " + code + " " + (out.error?.message ?? "planned"));
  return { ok: bad.length === 0, detail: bad.length ? bad.join("; ") : "typed, alone, channel " + plans.en.out.plan?.channel + ", reason en " + [...why.en].length + " / de " + [...why.de].length + " runes, in full; with an install refused" };
}

export async function marketplaceAccept(ctx) {
  const name = "Marketplace warning: planned typed and alone for the App's Supervisor user, full en/de reason";
  const [conf] = await ctx.ws([{ type: "get_config" }]);
  if (!atLeast(conf?.version, "2026.11")) return ctx.row(ctx.S, name, "SKIP", "Home Assistant " + conf?.version + " on the HAOS VM has no Marketplace (2026.11+): update its Home Assistant, set VM_LANE_HA_VERSION, rerun");
  const why = {};
  for (const lang of ["en", "de"]) {
    const res = await ctx.fetch("https://raw.githubusercontent.com/" + ctx.repo + "/" + ctx.tag + "/cli/confirm_strings." + lang + ".json");
    if (!res.ok) throw new Error(ctx.tag + " confirm_strings." + lang + ".json: HTTP " + res.status);
    why[lang] = (await res.json())[WHY];
  }
  const plan = async (items) => {
    const json = JSON.stringify(items.map((payload) => ({ endpoint: "ws", payload })));
    const r = await ctx.run("$f=Join-Path $env:TEMP 'vm-lane-marketplace.json'; [IO.File]::WriteAllText($f, '" + json + "')\nha-nova plan raw --file $f 2>$null; $rc=$LASTEXITCODE; Remove-Item -LiteralPath $f -Force; exit $rc", { timeoutMs: 3 * 60_000 });
    return { code: r.code, out: envelope(r.out) };
  };
  const plans = {}, was = conf.language;
  let current = was;
  try {
    for (const lang of ["en", "de"]) { // the reason comes in Home Assistant's language: switch it, then back
      if (current !== lang) { current = lang; await ctx.ws([{ type: "config/core/update", language: lang }]); }
      plans[lang] = await plan([ACCEPT]);
    }
    plans.bundled = await plan([ACCEPT, INSTALL]);
  } finally { // read back, whatever happened: a switch that timed out may still have applied
    let [after] = await ctx.ws([{ type: "get_config" }]);
    if (after?.language !== was) { await ctx.ws([{ type: "config/core/update", language: was }]); [after] = await ctx.ws([{ type: "get_config" }]); }
    ctx.check(ctx.S, "Home Assistant language back to " + was, after?.language === was, "now " + after?.language);
  }
  const { ok, detail } = judgeMarketplace(plans, why);
  return ctx.check(ctx.S, name, ok, detail + "; stops at the plan: Home Assistant can't withdraw an acceptance, and the App backup doesn't hold it");
}

// Row 2 (#1148): the check-up read every Supervisor stats path through the App.
export function judgeCheckup({ code, out }) {
  const result = envelope(out).result ?? {};
  const res = result.facts?.resources;
  const gaps = (result.not_checked ?? []).filter((g) => ["resources", "cpu"].includes(g.check));
  const top = res?.top_memory ?? [];
  const ok = code === 0 && gaps.length === 0 && typeof res?.cpu_percent === "number" && res.memory_mb > 0 && top.some((u) => u.kind === "core" && u.memory_mb > 0) && top.every((u) => typeof u.memory_mb === "number" && typeof u.cpu_percent === "number");
  const detail = code !== 0 ? "exit " + code + ": " + out.trim().split(/\r?\n/).slice(-2).join(" | ") : gaps.length ? "not checked: " + gaps.map((g) => g.check + " " + g.reason).join(", ") : "CPU " + res?.cpu_percent + " %, memory " + res?.memory_mb + " MB" + (res?.memory_percent !== undefined ? " (" + res.memory_percent + " %)" : "") + "; top " + top.map((u) => u.kind + " " + u.memory_mb + " MB").join(", ");
  return { ok, detail };
}

export async function checkupStats(ctx) {
  const r = await ctx.run("ha-nova map --section checkup 2>$null; exit $LASTEXITCODE", { timeoutMs: 3 * 60_000 });
  const { ok, detail } = judgeCheckup(r);
  return ctx.check(ctx.S, "check-up: the App reads /core, /supervisor and /addons/{slug}/stats (CPU, memory)", ok, detail);
}

// Row 3 (#1161): the waiting window's OK, clicked in the lane user's console
// session, and the address the browser was started with. OK is clicked by its
// control id once the CLI enabled it (WM_COMMAND IDOK), as a mouse click would.
export const DRIVER = [ // no here-string: desk() wraps the job in one
  "$sig='[DllImport(\"user32.dll\", CharSet = CharSet.Unicode)] public static extern IntPtr FindWindowW(string cls, string title);' +",
  "  '[DllImport(\"user32.dll\")] public static extern IntPtr GetDlgItem(IntPtr dlg, int id);' +",
  "  '[DllImport(\"user32.dll\")] public static extern bool IsWindowEnabled(IntPtr h);' +",
  "  '[DllImport(\"user32.dll\", CharSet = CharSet.Unicode)] public static extern int GetWindowTextW(IntPtr h, System.Text.StringBuilder text, int max);' +",
  "  '[DllImport(\"user32.dll\")] public static extern IntPtr SendMessageW(IntPtr h, uint msg, IntPtr w, IntPtr l);'",
  "Add-Type -Namespace VmLane -Name Box -MemberDefinition $sig",
  "$box=[IntPtr]::Zero; $ok=[IntPtr]::Zero; $end=(Get-Date).AddSeconds(75)",
  "while ((Get-Date) -lt $end) { $box=[VmLane.Box]::FindWindowW('#32770','HA NOVA'); if ($box -ne [IntPtr]::Zero) { $ok=[VmLane.Box]::GetDlgItem($box,1); if ($ok -ne [IntPtr]::Zero -and [VmLane.Box]::IsWindowEnabled($ok)) { break } }; Start-Sleep -Milliseconds 300 }",
  "if ($ok -eq [IntPtr]::Zero -or -not [VmLane.Box]::IsWindowEnabled($ok)) { 'vm-lane: no HA NOVA window with OK enabled'; exit 3 }",
  "$t=New-Object System.Text.StringBuilder 4096; [void][VmLane.Box]::GetWindowTextW([VmLane.Box]::GetDlgItem($box,0xFFFF),$t,4096)",
  "\"window=$($t.ToString() -replace '\\s+',' ')\"",
  // Only the waiting window, whose OK opens the page, is ever answered OK; any other box gets Cancel.
  "if ($t.ToString() -notmatch 'NOVA page; Cancel stops it|NOVA-Seite; Abbrechen stoppt sie') { [void][VmLane.Box]::SendMessageW($box,0x0010,[IntPtr]::Zero,[IntPtr]::Zero); 'vm-lane: not the waiting window that opens the NOVA page; closed it unanswered'; exit 5 }",
  "[void][VmLane.Box]::SendMessageW($box,0x0111,[IntPtr]1,$ok)",
  "$url=''; $end=(Get-Date).AddSeconds(20)",
  "while (-not $url -and (Get-Date) -lt $end) { foreach ($p in @(Get-CimInstance Win32_Process -Filter \"CommandLine LIKE '%nova_ask=%'\")) { $m=[regex]::Match([string]$p.CommandLine,'https?://[^\\s\"'']+'); if ($m.Success -and $m.Value -like '*nova_ask=*') { $url=$m.Value; break } }; if (-not $url) { Start-Sleep -Milliseconds 150 } }",
  "\"opened=$url\"",
  "foreach ($n in 'rundll32.exe','msedge.exe') { taskkill.exe /F /T /FI \"USERNAME eq $env:USERNAME\" /IM $n 2>$null | Out-Null }",
  "if (-not $url) { exit 4 }",
].join("\n");

// The asks on the approval view, in its order: [{ ref, text }]; text is what the card says
// before its buttons (0.30 and 0.31 Apps word it differently).
export function approvalAsks(html) {
  return [...html.matchAll(/<section[^>]*>([\s\S]*?)<\/section>/g)].map((m) => ({
    ref: m[1].match(/name="ref" value="([0-9a-f]{64})"/)?.[1] ?? "",
    text: m[1].split("<form")[0].replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim(),
  })).filter((a) => a.ref);
}

// What the window opened, against the NOVA page and approval view for its ref.
// opened: the URL; page: the NOVA page; approval: the view at <data-ask>?nova_ask=<ref>.
export function judgeWindowPage({ opened, guestHa, app, ingress, page, approval, panelStatus }) {
  let url;
  try { url = new URL(opened); } catch { return { ok: false, ref: "", detail: "the window's OK opened no NOVA address" + (opened ? ": " + opened : "") }; }
  const ref = url.searchParams.get("nova_ask") ?? "";
  const bad = [];
  if (url.origin !== new URL(guestHa).origin || url.pathname !== "/app/" + app || [...url.searchParams.keys()].join() !== "nova_ask" || !REF.test(ref)) bad.push("opened " + url.href + ", not " + guestHa + "/app/" + app + "?nova_ask=<ref>");
  if (panelStatus !== 200) bad.push("Home Assistant answers " + panelStatus + " for /app/" + app);
  const ask = page?.match(/data-ask="([^"]+)"/)?.[1];
  // Supervisor's ingress_url ends in the App's ingress_entry (/home); the page's ask hangs off the ingress root.
  const want = (ingress.match(/^\/api\/hassio_ingress\/[^/]+/)?.[0] ?? ingress) + "/approve";
  if (ask !== want) bad.push("the NOVA page's data-ask is " + (ask ?? "missing") + ", not " + want);
  if (!page?.includes('top.location.replace(b.dataset.ask+"?nova_ask="')) bad.push("the NOVA page has no hand-off script (" + (page?.length ?? 0) + " bytes)");
  const first = approvalAsks(approval ?? "")[0];
  if (!first || first.ref !== ref) bad.push("the approval view lists " + (first ? "another ask" : "no ask") + " first");
  else if (!/front.door/i.test(first.text)) bad.push("the approval view's ask is not the unlock: " + first.text);
  if (!(approval ?? "").includes("approve?nova_ask=" + ref + '"')) bad.push("the approval view's reload drops nova_ask");
  return { ok: REF.test(ref) && bad.length === 0, ref, detail: bad.length ? bad.join("; ") : url.origin + url.pathname + "?nova_ask=" + ref.slice(0, 8) + "…, first on the view: " + first.text };
}

export async function windowButton(ctx) {
  const S = ctx.S, lock = "lock.front_door";
  const page = await ctx.nova();
  const before = await ctx.states([lock]);
  if (before[lock] !== "locked") return ctx.row(S, "window button: unlock planned typed, asked through the App", "FAIL", lock + " is " + before[lock] + ", not locked: lock it on the HAOS VM and rerun");
  const open = approvalAsks(await page.approval()).map((a) => a.ref);
  const p = await ctx.run("ha-nova plan call lock.unlock " + lock + " 2>$null; exit $LASTEXITCODE", { timeoutMs: 3 * 60_000 });
  const planned = envelope(p.out);
  if (!ctx.check(S, "window button: unlock planned typed, asked through the App", p.code === 3 && planned.tier === "typed" && planned.plan?.channel === "relay" && PLAN_ID.test(planned.plan?.id ?? ""), "exit " + p.code + ", tier " + planned.tier + ", channel " + planned.plan?.channel + (planned.error ? ", " + planned.error.code : ""))) return;
  const applying = ctx.run("ha-nova apply --plan " + planned.plan.id + "; exit $LASTEXITCODE", { timeoutMs: 4 * 60_000 }).catch((err) => ({ code: -1, out: String(err) }));
  let judged = { ok: false, ref: "", detail: "" }, drove = { code: -1, out: "" }, cancelled = [];
  try {
    drove = await ctx.run(DRIVER, { timeoutMs: 2 * 60_000 });
    const opened = drove.out.match(/^opened=(\S*)/m)?.[1] ?? "";
    const ref = opened.match(/nova_ask=([0-9a-f]{64})/)?.[1] ?? "";
    const panel = opened ? await ctx.fetch(ctx.ha + "/app/" + ctx.app + "?nova_ask=" + ref).then((r) => r.status, () => 0) : 0;
    judged = judgeWindowPage({ opened, guestHa: ctx.guestHa, app: ctx.app, ingress: page.ingress, page: await page.html(), approval: REF.test(ref) ? await page.approval(ref) : "", panelStatus: panel });
  } finally {
    for (let i = 0; i < 20 && !cancelled.length; i++) { // end the asks this row opened, on the page, as a person's Cancel
      const mine = approvalAsks(await page.approval()).map((a) => a.ref).filter((r) => !open.includes(r));
      for (const r of mine) { await page.cancelAsk(r); cancelled.push(r); }
      if (!cancelled.length) await new Promise((done) => setTimeout(done, 3000)); // not ctx.sleep: an abort must not skip the Cancel
    }
  }
  const windowText = drove.out.match(/^window=(.*)$/m)?.[1] ?? "";
  ctx.check(S, "window button: opens exactly this ask on the NOVA approval page", judged.ok, judged.detail + (windowText ? "; window: " + windowText.slice(0, 80) : "; " + drove.out.trim().split(/\r?\n/).slice(-1)[0]));
  const applied = await applying;
  const after = await ctx.states([lock]);
  return ctx.check(S, "window ask cancelled on the NOVA page: nothing sent", cancelled.length === 1 && cancelled[0] === (judged.ref || cancelled[0]) && applied.code !== 0 && /CONFIRM_DECLINED/.test(applied.out) && after[lock] === before[lock], "cancelled " + cancelled.length + " ask(s); apply exit " + applied.code + " " + (envelope(applied.out).error?.code ?? "") + "; " + lock + " " + before[lock] + " -> " + after[lock]);
}

// The three rows in order; one that throws fails alone.
export async function appAsks(ctx) {
  for (const [name, fn] of [["Marketplace warning", marketplaceAccept], ["check-up stats", checkupStats], ["window button", windowButton]]) {
    try { await fn(ctx); } catch (err) { ctx.row(ctx.S, name, "FAIL", err.message); }
  }
}
