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
