// The HAOS-only pass (--container): when this host can't hold a second VM,
// the rows that need a computer paired with the App run in a throwaway Linux
// container here instead of the Win11 or Ubuntu VM (with an unlocked login
// keyring, as on a desktop; no AI app, so no setup or client wiring): install, pair, update,
// a read and a call, the late approval on the NOVA page (#1183), the
// check-up's stats reads, and the guided App update (with --app-branch).
// Spec: docs/work/2026-10-26-vm-lane-spec.md → "HAOS-only pass".
import { spawn } from "node:child_process";
import { appendFileSync } from "node:fs";
import { join } from "node:path";
import { judgeCheckup, lateApproval } from "./app-asks.mjs";

// The container reaches the HAOS VM's forwards on this host's loopback (8123, 8791, and
// 8792, where pairing activates the device over TLS).
export const CONTAINER_HA = "http://host.docker.internal:8123";
export const CONTAINER_RELAY = "http://host.docker.internal:8791";
const IMAGE = "ubuntu:24.04";
const USER = "lane";

// Out: only the host's forwards (Home Assistant, the Relay and its TLS port), DNS, and the internet (the installers). No
// LAN and no multicast, so nothing finds a real Home Assistant.
const ISOLATE = `H=$(getent ahostsv4 host.docker.internal | awk 'NR==1{print $1}')
D=$(awk '/^nameserver/{print $2; exit}' /etc/resolv.conf)
[ -n "$H" ] && [ -n "$D" ]
iptables -A OUTPUT -o lo -j ACCEPT
iptables -A OUTPUT -d "$H" -p tcp -m multiport --dports 8123,8791,8792 -j ACCEPT
iptables -A OUTPUT -d "$D" -p udp --dport 53 -j ACCEPT
iptables -A OUTPUT -d "$D" -p tcp --dport 53 -j ACCEPT
for n in 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 100.64.0.0/10 169.254.0.0/16 224.0.0.0/4 255.255.255.255; do iptables -A OUTPUT -d "$n" -j REJECT; done
ip6tables -A OUTPUT -o lo -j ACCEPT 2>/dev/null; ip6tables -P OUTPUT DROP 2>/dev/null || true
lan=$(curl -s -m 3 -o /dev/null -w '%{http_code}' http://192.168.0.1/ || true)
ha=$(curl -s -m 10 -o /dev/null -w '%{http_code}' ${CONTAINER_HA}/api/ || true)
relay=$(curl -s -m 10 -o /dev/null -w '%{http_code}' ${CONTAINER_RELAY}/health || true)
echo "rules=$(iptables -S OUTPUT | grep -c '^-A') lan=$lan ha=$ha relay=$relay"`;

// box: one container for the run, started on first use. sh(script) runs bash as the
// lane user; tty(cmd) runs cmd in a pseudo-terminal (script) whose input the caller writes.
export function linuxBox({ work, children, clean }) {
  const name = `ha-nova-vm-lane-${process.pid}`;
  let started;
  const run = (args, { input, tty = false, timeoutMs = 10 * 60_000, onData } = {}) => new Promise((done) => {
    const child = spawn("docker", args, { stdio: ["pipe", "pipe", "pipe"] });
    children.add(child);
    child.stdin.on("error", () => {});
    let out = "";
    const timer = setTimeout(() => { out += "\n[vm-lane: timed out]\n"; child.kill("SIGKILL"); }, timeoutMs);
    const take = (d) => { out += d; onData?.(out, child); };
    child.stdout.on("data", take);
    child.stderr.on("data", take);
    child.on("close", (code) => { clearTimeout(timer); children.delete(child); appendFileSync(join(work, "container.log"), `\n$ docker ${args.slice(0, 6).join(" ")}\n${clean(out)}\n[exit ${code}]\n`); done({ code, out: clean(out) }); });
    if (!tty) child.stdin.end(input ?? "");
  });
  const exec = (user, cmd, o) => run(["exec", "-i", "-u", user, name, ...cmd], o);
  return {
    start: () => (started ??= (async () => {
      const up = await run(["run", "-d", "--rm", "--name", name, "--cap-add", "NET_ADMIN", "--add-host", "host.docker.internal:host-gateway", IMAGE, "sleep", "infinity"], { timeoutMs: 5 * 60_000 });
      if (up.code !== 0) return { ok: false, detail: up.out.trim().split("\n").slice(-1)[0] };
      const r = await exec("root", ["bash", "-s"], { timeoutMs: 6 * 60_000, input: `set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq && apt-get install -y -qq --no-install-recommends curl ca-certificates iptables dbus gnome-keyring >/dev/null
useradd -m ${USER}
install -d -o ${USER} -m 700 /run/user/$(id -u ${USER})
${ISOLATE}` });
      // The user's session bus where HA NOVA looks for it (/run/user/<uid>/bus), with an
      // unlocked login keyring on it: pairing needs one, as on a desktop.
      const k = r.code === 0 ? await exec(USER, ["sh", "-c", "export XDG_RUNTIME_DIR=/run/user/$(id -u); B=$XDG_RUNTIME_DIR/bus; dbus-daemon --session --fork --address=unix:path=$B && printf lane | DBUS_SESSION_BUS_ADDRESS=unix:path=$B gnome-keyring-daemon --unlock --components=secrets >/dev/null && echo keyring=open"], { timeoutMs: 60_000 }) : { out: "" };
      const m = r.out.match(/rules=(\d+) lan=(\d+) ha=(\d+) relay=(\d+)/);
      const ok = r.code === 0 && k.out.includes("keyring=open") && Number(m?.[1]) >= 11 && m?.[2] === "000" && m?.[3] === "401" && m?.[4] !== "000";
      return { ok, detail: m ? `${IMAGE}, ${m[1]} outbound rules, LAN probe ${m[2] === "000" ? "refused" : "HTTP " + m[2]}, HAOS forward HTTP ${m[3]}, Relay forward HTTP ${m[4]}, login keyring ${k.out.includes("keyring=open") ? "open" : "NOT OPEN"}` : r.out.trim().split("\n").slice(-2).join(" | ") };
    })()),
    sh: (script, o = {}) => exec(USER, ["bash", "-s"], { input: script, ...o }),
    tty: (cmd, o = {}) => exec(USER, ["script", "-qfec", cmd, "/dev/null"], { tty: true, ...o }),
    stop: () => started ? run(["rm", "-f", name], { timeoutMs: 60_000 }) : Promise.resolve({ code: 0, out: "" }),
  };
}

const last = (s, n = 1) => s.trim().split(/\r?\n/).filter(Boolean).slice(-n).join(" | ");

// ctx: { S, box, tag, stable, repo, nova, lock(service), check, sleep }
export async function containerRun(ctx) {
  const { S, box, tag, stable } = ctx;
  const up = await box.start();
  if (!ctx.check(S, "Linux container: no LAN, the HAOS VM's forwards reachable", up.ok, up.detail)) return;
  const ENV = `env -i HOME=/home/${USER} USER=${USER} LOGNAME=${USER} LANG=C.UTF-8 TERM=dumb SHELL=/bin/bash PATH=/home/${USER}/.local/bin:/usr/local/bin:/usr/bin:/bin`;
  const sh = (cmd, o) => box.sh(`set -o pipefail; ${cmd}`, o);
  const hn = (args, o) => sh(`${ENV} ha-nova ${args} </dev/null`, o);
  const install = (t) => sh(`curl -fsSL https://raw.githubusercontent.com/${ctx.repo}/${t}/install.sh | ${ENV} HA_NOVA_VERSION=${t} bash`, { timeoutMs: 6 * 60_000 });
  const i1 = await install(stable);
  const v1 = await hn("version");
  if (!ctx.check(S, `${stable} one-liner (no terminal): installs`, i1.code === 0 && v1.out.trim() === stable.slice(1), `${last(i1.out)}; version ${last(v1.out)}`)) return;
  const p = await hn(`pair --relay-url ${CONTAINER_RELAY} --code ${await (await ctx.nova()).code()} --credential-store=file`, { timeoutMs: 3 * 60_000 });
  if (!ctx.check(S, "pair with a six-digit code from the NOVA page", p.code === 0, last(p.out, 2))) return;
  const u = await hn(`update --version ${tag}`, { timeoutMs: 5 * 60_000 });
  const v2 = await hn("version");
  ctx.check(S, `ha-nova update --version ${tag} (from ${stable})`, u.code === 0 && v2.out.trim() === tag.slice(1), `${last(u.out)}; now ${last(v2.out)}`);
  const d = await hn("doctor", { timeoutMs: 3 * 60_000 });
  ctx.check(S, `doctor on ${tag}`, d.code === 0, last(d.out));
  const g = await hn("get lock.front_door");
  ctx.check(S, "read: ha-nova get lock.front_door", g.code === 0 && /"state"\s*:\s*"(locked|unlocked)"/.test(g.out), last(g.out).slice(0, 120));
  const on = await hn("call light.turn_on light.bed_light"), off = await hn("call light.turn_off light.bed_light");
  ctx.check(S, "ordinary call: light.turn_on / turn_off bed light", on.code === 0 && off.code === 0 && /"status"\s*:\s*"verified"/.test(on.out) && /"status"\s*:\s*"verified"/.test(off.out), `exit ${on.code} / ${off.code}`);
  await lateApproval({ S, hn, nova: ctx.nova, lock: ctx.lock, check: ctx.check, sleep: ctx.sleep });
  const { ok, detail } = judgeCheckup(await hn("map --section checkup 2>/dev/null", { timeoutMs: 3 * 60_000 }));
  ctx.check(S, "check-up: the App reads /core, /supervisor and /addons/{slug}/stats (CPU, memory)", ok, detail);
  const before = (await (await ctx.nova()).devices()).length;
  const pg = await hn("uninstall --yes --purge", { timeoutMs: 3 * 60_000 });
  const left = await sh(`find /home/${USER} -name '*ha-nova*' ! -name '.ha-nova-*' | head -5`);
  const devs = (await (await ctx.nova()).devices()).length;
  ctx.check(S, "uninstall --purge: all gone, device revoked", pg.code === 0 && !left.out.trim() && devs === before - 1, `${last(pg.out)}; leftovers: ${left.out.trim() || "none"}; NOVA devices ${before} -> ${devs}`);
}
