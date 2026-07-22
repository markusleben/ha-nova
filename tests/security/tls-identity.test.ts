import { createHash } from "node:crypto";
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, unlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { connect } from "node:tls";

import { createServer } from "node:https";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const storageMock = vi.hoisted(() => ({
  actualWriteFileAtomicSync: undefined as
    | ((path: string, data: Buffer | string) => void)
    | undefined,
  writeFileAtomicSync: vi.fn<(path: string, data: Buffer | string) => void>(),
}));

vi.mock("../../nova/src/storage/atomic-file.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../nova/src/storage/atomic-file.js")>();
  storageMock.actualWriteFileAtomicSync = actual.writeFileAtomicSync;
  storageMock.writeFileAtomicSync.mockImplementation(actual.writeFileAtomicSync);
  return { ...actual, writeFileAtomicSync: storageMock.writeFileAtomicSync };
});

import {
  TlsIdentityCorruptError,
  loadOrCreateTlsIdentity,
  type TlsIdentity,
} from "../../nova/src/security/tls-identity.js";

// Disposable test identity generated once with @peculiar/x509 1.12.3. This
// pins the persisted on-disk format across the v2 parser migration without
// committing standalone key files.
const V1_KEY_DER_BASE64 =
  "MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgArSZttQUP+/fq67RcAaABIrKKjGgvOlyZz0gwtuPufuhRANCAAS19FeMXhI9p1x59dG9LQ5BSHeY3+2ibDtEbnd/GZI87XjKDvsHax+PKMvY2OK/govBTICc4KbjwPEZJsk0yh4z";
const V1_CERT_DER_BASE64 =
  "MIIBdTCCARugAwIBAgIIASNFZ4mrze8wCgYIKoZIzj0EAwIwIDEeMBwGA1UEAxMVbm92YS1yZWxheS12MS1maXh0dXJlMB4XDTI2MDEwMTAwMDAwMFoXDTM2MDEwMTAwMDAwMFowIDEeMBwGA1UEAxMVbm92YS1yZWxheS12MS1maXh0dXJlMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEtfRXjF4SPadcefXRvS0OQUh3mN/tomw7RG53fxmSPO14yg77B2sfjyjL2Njiv4KLwUyAnOCm48DxGSbJNMoeM6M/MD0wDAYDVR0TAQH/BAIwADAOBgNVHQ8BAf8EBAMCBaAwHQYDVR0OBBYEFHxmyLD48lhf+8f3Pyn1tS1u/l6OMAoGCCqGSM49BAMCA0gAMEUCIQDjHBvRVn/mlyxfB5l1/MCIFApxCqHDJfUFFu6igBwJ3AIgJSLnaUiugzblHnGoMcbxH+ZdbnV1ISGDatLMLoLX5XE=";
const V1_SPKI_PIN = "0_EzkFSqRs0SLug7pkcvidBx4bVC537dgq-qImfDj0I";

let dir: string;
beforeEach(() => {
  const actualWrite = storageMock.actualWriteFileAtomicSync;
  if (!actualWrite) {
    throw new Error("atomic file mock was not initialized");
  }
  storageMock.writeFileAtomicSync.mockReset();
  storageMock.writeFileAtomicSync.mockImplementation(actualWrite);
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
    expect(await spkiPinSeenOverTls(id)).toBe(id.spkiPin);
  });

  it("loads a v1.12.3 identity without changing its pin or persisted bytes", async () => {
    const keyPem = pemFromDer("PRIVATE KEY", Buffer.from(V1_KEY_DER_BASE64, "base64"));
    const certPem = pemFromDer("CERTIFICATE", Buffer.from(V1_CERT_DER_BASE64, "base64"));
    const keyPath = join(dir, "tls-key.pem");
    const certPath = join(dir, "tls-cert.pem");
    writeFileSync(keyPath, keyPem);
    writeFileSync(certPath, certPem);

    const keyBefore = readFileSync(keyPath);
    const certBefore = readFileSync(certPath);
    const keyInodeBefore = statSync(keyPath).ino;
    const certInodeBefore = statSync(certPath).ino;
    const loaded = await loadOrCreateTlsIdentity(dir);

    expect(loaded.spkiPin).toBe(V1_SPKI_PIN);
    expect(readFileSync(keyPath)).toEqual(keyBefore);
    expect(readFileSync(certPath)).toEqual(certBefore);
    expect(statSync(keyPath).ino).toBe(keyInodeBefore);
    expect(statSync(certPath).ino).toBe(certInodeBefore);
    expect(await spkiPinSeenOverTls(loaded)).toBe(V1_SPKI_PIN);
  });

  it("regenerates when only the certificate is present (a keyless cert cannot serve TLS)", async () => {
    const first = await loadOrCreateTlsIdentity(dir);
    unlinkSync(join(dir, "tls-key.pem")); // first-run write killed, or a partial /data restore
    // A cert without its key is unusable, so regenerate rather than brick the App
    // before the owner console can start; the pin rotates, forcing a re-pair.
    const regenerated = await loadOrCreateTlsIdentity(dir);
    expect(existsSync(join(dir, "tls-key.pem"))).toBe(true);
    expect(existsSync(join(dir, "tls-cert.pem"))).toBe(true);
    expect(regenerated.spkiPin).not.toBe(first.spkiPin);
  });

  it("regenerates when only the key is present", async () => {
    const first = await loadOrCreateTlsIdentity(dir);
    unlinkSync(join(dir, "tls-cert.pem"));
    const regenerated = await loadOrCreateTlsIdentity(dir);
    expect(existsSync(join(dir, "tls-cert.pem"))).toBe(true);
    expect(regenerated.spkiPin).not.toBe(first.spkiPin);
  });

  it("keeps cert-only recovery retryable when the matching key write fails", async () => {
    const first = await loadOrCreateTlsIdentity(dir);
    const keyPath = join(dir, "tls-key.pem");
    const certPath = join(dir, "tls-cert.pem");
    unlinkSync(keyPath);
    const staleCert = readFileSync(certPath);

    const actualWrite = storageMock.actualWriteFileAtomicSync;
    if (!actualWrite) {
      throw new Error("atomic file mock was not initialized");
    }
    let writes = 0;
    storageMock.writeFileAtomicSync.mockImplementation((path, data) => {
      writes += 1;
      if (writes === 2) {
        throw new Error("injected key write failure");
      }
      actualWrite(path, data);
    });

    await expect(loadOrCreateTlsIdentity(dir)).rejects.toThrow("injected key write failure");
    expect(writes).toBe(2);
    expect(existsSync(keyPath)).toBe(false);
    expect(readFileSync(certPath)).not.toEqual(staleCert);

    storageMock.writeFileAtomicSync.mockImplementation(actualWrite);
    const recovered = await loadOrCreateTlsIdentity(dir);
    expect(existsSync(keyPath)).toBe(true);
    expect(existsSync(certPath)).toBe(true);
    expect(recovered.spkiPin).not.toBe(first.spkiPin);
    const reloaded = await loadOrCreateTlsIdentity(dir);
    expect(reloaded.spkiPin).toBe(recovered.spkiPin);
    expect(await spkiPinSeenOverTls(reloaded)).toBe(recovered.spkiPin);
  });

  it("fail-closed on an unparseable certificate", async () => {
    await loadOrCreateTlsIdentity(dir);
    writeFileSync(join(dir, "tls-cert.pem"), "-----BEGIN CERTIFICATE-----\ngarbage\n-----END CERTIFICATE-----\n");
    await expect(loadOrCreateTlsIdentity(dir)).rejects.toBeInstanceOf(TlsIdentityCorruptError);
  });

  it("fail-closed when a parseable certificate contains malformed SPKI parameters", async () => {
    const identity = await loadOrCreateTlsIdentity(dir);
    writeFileSync(join(dir, "tls-cert.pem"), invalidateEcCurveParameters(identity.certPem));
    await expect(loadOrCreateTlsIdentity(dir)).rejects.toBeInstanceOf(TlsIdentityCorruptError);
  });

  it("fail-closed on an unparseable private key", async () => {
    await loadOrCreateTlsIdentity(dir);
    writeFileSync(join(dir, "tls-key.pem"), "-----BEGIN PRIVATE KEY-----\ngarbage\n-----END PRIVATE KEY-----\n");
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

function pemFromDer(label: string, der: Buffer): string {
  const body = der.toString("base64").replace(/(.{64})/g, "$1\n");
  return `-----BEGIN ${label}-----\n${body}\n-----END ${label}-----\n`;
}

function invalidateEcCurveParameters(certPem: string): string {
  const der = Buffer.from(certPem.replace(/-----[^-]+-----|\s/g, ""), "base64");
  const p256CurveOid = Buffer.from("06082a8648ce3d030107", "hex");
  const offset = der.indexOf(p256CurveOid);
  expect(offset).toBeGreaterThanOrEqual(0);
  // Keep the outer certificate parseable while making the lazy PublicKey
  // decoder reject the required EC parameters.
  der[offset] = 0x05;
  return pemFromDer("CERTIFICATE", der);
}

async function spkiPinSeenOverTls(identity: TlsIdentity): Promise<string> {
  const server = createServer(
    { key: identity.keyPem, cert: identity.certPem, minVersion: "TLSv1.3", maxVersion: "TLSv1.3" },
    (_req, res) => res.end("ok")
  );
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const port = (server.address() as { port: number }).port;

  try {
    return await new Promise<string>((resolve, reject) => {
      const socket = connect(
        // Trust the self-signed leaf as its own CA and skip only the hostname
        // check — the relay's TLS is pin-based, not CA/hostname-based, but
        // disabling verification outright trips security scanners.
        {
          host: "127.0.0.1",
          port,
          ca: identity.certPem,
          checkServerIdentity: () => undefined,
          minVersion: "TLSv1.3",
        },
        () => {
          socket.setTimeout(0);
          try {
            const cert = socket.getPeerX509Certificate();
            const proto = socket.getProtocol();
            if (!cert || proto !== "TLSv1.3") {
              throw new Error("no cert or not TLS1.3");
            }
            resolve(
              createHash("sha256")
                .update(cert.publicKey.export({ type: "spki", format: "der" }))
                .digest("base64url")
            );
          } catch (error) {
            reject(error);
          } finally {
            socket.end();
          }
        }
      );
      socket.setTimeout(5_000, () => {
        socket.destroy();
        reject(new Error("TLS handshake timed out"));
      });
      socket.once("error", reject);
    });
  } finally {
    await new Promise<void>((resolve, reject) => {
      server.close((error) => (error ? reject(error) : resolve()));
    });
  }
}
