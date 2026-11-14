# Automated VM release lane (2026-10-26)

Status: active

`npm run vm-lane -- <tag>` replaces the Windows 11 manual onboarding
proof. It uses the maintainer-started throwaway Win11 and HAOS UTM VMs;
Ubuntu is optional. The Cloud accounts lane and real phones remain manual.
The requested single-script shape keeps the platform orchestration together;
it exceeds the usual approximate 400-line target rather than adding helpers.

## Contract

- Before writes, require the runbook's loopback forwards, HAOS QEMU UUID,
  location, timezone, authenticated HA configuration, and demo entity sources.
  Reject pending discovery flows. Back up the NOVA App before pairing or
  updating it; restore that backup and original entity states at cleanup.
  Verify the complete device registry's canonical SHA-256, excluding only
  device usage timestamps which the chat bridge updates on restart, before
  removing the lane's backup. Retain every credential, device, revocation
  and history field in that comparison; keep the backup if restoration fails.
- Win11 uses fresh standard users, x64 PowerShell 7 for the public installer,
  and PowerShell 7/5.1 for the CLI lifecycle. Native ARM64 5.1 must give its
  documented x64 installer guidance. The lane is not a native amd64 5.1
  installer proof or a Windows 10 proof.
- Prove candidate and stable public installer handoff to the first setup
  prompt, six-digit NOVA-page pairing, noninteractive setup for installed
  clients, doctor, stable-to-candidate update, a read and verified ordinary
  call, wiring, rerun, standard uninstall, stale marker, reinstall, and purge.
  No fake client qualifies for Windows onboarding. The remainder of the
  setup wizard and native confirmation-window interactions are outside this
  lane.
- Windows native credential operations run as the lane user in the signed-in
  console session through a one-shot scheduled task. SSH only controls it.
  Random lane passwords are never committed or printed. Job files and tasks
  are removed; accounts are deleted. Windows may keep deleted users' profiles
  loaded until a guest reboot; report that explicitly, never reboot the VM.
- Recreate the VM's outbound LAN isolation rules before discovery. Ubuntu
  must have the runbook's effective nftables output rules. Never issue a
  request to the maintainer's real HA, even as an isolation probe.
- Windows scripts reach the guest as files (scp to the user's home, then
  `-File`), never over stdin: on the Win11 guest a stdin read can stay
  blocked after EOF. A script's first statement claims its file by renaming
  it; a job with no start within 30 s is voided the same way. Only a won void
  proves the job never started, and only that case is retried, once. A second
  miss reads STALL: a harness state, not a product result.
- Never run a HA NOVA command on macOS. Never start/stop UTM or shut down a
  guest. Print STEP/CHECK/RESULT/DETAIL; FAIL or STALL means a nonzero exit.
  Retain failed-run logs. `--wait 60` checks required forwards every two
  minutes.

## Verification

Run the lane against published `v0.28.0-rc1`, starting from `v0.27.3`.
Run syntax, TypeScript and documentation checks, plus read-only adversarial
and local CLI reviews. Record the real VM table and limits with the branch
handoff; do not open a PR for this task.

## 0.31 rows (MAR-144)

Four rows check 0.31 fixes on the throwaway VMs against the App preview
(`--app-branch`). Each row fails on the defect it guards and passes on the
fix. `tests/e2e/vm-lane-rc-rows.test.ts` judges recorded shapes of both.
They run in the Win11 PowerShell 7 user after its core rows
(`app-asks.mjs`), except row 4 (`guided-update.mjs`).

1. **Marketplace warning (#1132).** `plan raw` of
   `marketplace/warning/accept` must be typed and alone, with one item.
   It runs as the App's Supervisor user (the row doesn't check a
   person's account). Its reason must be the full `raw.why.marketplace_warning`
   text of the tag in English and German, and it must fit the phone's 300
   runes. The row switches Home Assistant's language to `de` and back,
   and checks the language is restored. Bundled with an install, the plan
   must be refused. The row stops at the plan: Home Assistant can't
   withdraw an acceptance, and the App backup doesn't hold it. Before
   Home Assistant 2026.11 (no Marketplace) the row is SKIP.
2. **Check-up stats (#1148).** `ha-nova map --section checkup` must leave
   neither `resources` nor `cpu` unchecked. It must report CPU and
   memory for Home Assistant and the top users. The check-up reads
   `/core/stats`, `/supervisor/stats` and each running App's
   `/addons/{slug}/stats`, and any failed read leaves `resources`
   unchecked.
3. **Window button (#1161).** It plans `lock.unlock lock.front_door`
   (typed, channel `relay`) and starts `apply`. A second job in the
   same console session waits for the HA NOVA message box. Once the CLI
   enables OK, the job clicks it (WM_COMMAND IDOK) and reads the address
   the browser was started with. That address must be exactly
   `<guest HA>/app/<slug>?nova_ask=<64 hex>`, and Home Assistant must
   answer 200 for it (its app panel route). The driver answers OK only on
   the waiting window whose text says OK opens the NOVA page; any other
   box gets Cancel and the row fails. The lock must start locked. The NOVA page must carry
   `data-ask=<ingress>/approve` and its hand-off script. The approval
   view at `?nova_ask=<ref>` must list that unlock first and keep the
   ref in its reload URL. The lane then cancels the ask on the approval
   view, as a person would. `apply` must end `CONFIRM_DECLINED`, and
   the lock's state must be unchanged. Not proven: Home Assistant's
   frontend framing the panel and running the hand-off script in a
   browser (no browser is driven).
4. **Guided App update (#1162).** This row needs a terminal and a newer
   App offered. So it runs inside the App preview step after the store
   offers the preview and before the lane installs it
   (`appPreview`'s `beforeUpdate`). It runs on the Ubuntu VM: a
   terminal session over SSH with a file credential store, in its own
   throwaway HOME and after its isolation check. Home Assistant must show
   the App's update entity offering the preview. An automation listening
   for `call_service` with `domain: update, service: install` (action:
   unlock the demo front door) is created. `ha-nova
   internal-offer-app-update` (the App step of `ha-nova update`) then
   runs in a pty. It must never show the yes question or the refusal. It
   must print the typed ask. The lane cancels that ask on the approval
   view (or skips the code an ungated App shows). The CLI must then say
   nothing was installed, and the App version must be unchanged. The
   automation is deleted (config and registry entry checked) and the
   HOME purged, which revokes its device. Pairing is then judged across
   the update from that point. The row is SKIP without `--app-branch`,
   with `--no-ubuntu`, or without the Ubuntu VM.

Rows 3 and 4 cancel one ask each, so they stay below the gate's pause
and hourly limits. Every device, automation and language change they
make is undone in the row. The lane's cleanup (devices revoked, entity
states as found, App backup restored, registry hash) still runs after
them.
