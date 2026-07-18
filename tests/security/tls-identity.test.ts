import { createHash } from "node:crypto";
import { mkdtempSync, rmSync, statSync, unlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { connect } from "node:tls";

import { createServer } from "node:https";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { TlsIdentityCorruptError, loadOrCreateTlsIdentity } from "../../nova/src/security/tls-identity.js";

let dir: string;
beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-tls-"));
});
afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

describe("tls-identity", () => {
  it("generates a P-256 identity, persists it 0600, and returns a stable SPKI pin", async () => {
    const first = await loadOrCreateTlsIdentity(dir);
    expect(first.spkiPin).toMatch(/^[A-Za-z0-9_-]{43}$/); // base64url SHA-256, no padding
    expect(statSync(join(dir, "tls-key.pem")).mode & 0o777).toBe(0o600);

    const second = await loadOrCreateTlsIdentity(dir);
    expect(second.spkiPin).toBe(first.spkiPin);
    expect(second.certPem).toBe(first.certPem);
  });

  it("serves TLS 1.3 whose leaf SPKI matches the advertised pin", async () => {
    const id = await loadOrCreateTlsIdentity(dir);
    const server = createServer(
      { key: id.keyPem, cert: id.certPem, minVersion: "TLSv1.3", maxVersion: "TLSv1.3" },
      (_req, res) => res.end("ok")
    );
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
    const port = (server.address() as { port: number }).port;

    const pin = await new Promise<string>((resolve, reject) => {
      const socket = connect(
        // Trust the self-signed leaf as its own CA and skip only the hostname
        // check — the relay's TLS is pin-based, not CA/hostname-based, but
        // disabling verification outright trips security scanners.
        { host: "127.0.0.1", port, ca: id.certPem, checkServerIdentity: () => undefined, minVersion: "TLSv1.3" },
        () => {
          const cert = socket.getPeerX509Certificate();
          const proto = socket.getProtocol();
          socket.end();
          if (!cert || proto !== "TLSv1.3") {
            reject(new Error("no cert or not TLS1.3"));
            return;
          }
          resolve(createHash("sha256").update(cert.publicKey.export({ type: "spki", format: "der" })).digest("base64url"));
        }
      );
      socket.on("error", reject);
    });
    server.close();
    expect(pin).toBe(id.spkiPin);
  });

  it("fail-closed when only the certificate is present (no silent pin rotation)", async () => {
    const id = await loadOrCreateTlsIdentity(dir);
    unlinkSync(join(dir, "tls-key.pem")); // partial /data restore
    await expect(loadOrCreateTlsIdentity(dir)).rejects.toBeInstanceOf(TlsIdentityCorruptError);
    // The still-present cert (and thus the original pin) is untouched.
    void id;
  });

  it("fail-closed when only the key is present", async () => {
    await loadOrCreateTlsIdentity(dir);
    unlinkSync(join(dir, "tls-cert.pem"));
    await expect(loadOrCreateTlsIdentity(dir)).rejects.toBeInstanceOf(TlsIdentityCorruptError);
  });

  it("fail-closed on an unparseable certificate", async () => {
    await loadOrCreateTlsIdentity(dir);
    writeFileSync(join(dir, "tls-cert.pem"), "-----BEGIN CERTIFICATE-----\ngarbage\n-----END CERTIFICATE-----\n");
    await expect(loadOrCreateTlsIdentity(dir)).rejects.toBeInstanceOf(TlsIdentityCorruptError);
  });

  it("fail-closed on a mixed restore (key and cert from different generations)", async () => {
    const first = await loadOrCreateTlsIdentity(dir);
    const otherDir = mkdtempSync(join(tmpdir(), "ha-nova-tls-b-"));
    try {
      const second = await loadOrCreateTlsIdentity(otherDir);
      // Cert from generation B over key from generation A.
      writeFileSync(join(dir, "tls-cert.pem"), second.certPem);
      await expect(loadOrCreateTlsIdentity(dir)).rejects.toBeInstanceOf(TlsIdentityCorruptError);
      expect(first.spkiPin).not.toBe(second.spkiPin);
    } finally {
      rmSync(otherDir, { recursive: true, force: true });
    }
  });
});
