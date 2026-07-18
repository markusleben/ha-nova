import { createHash, createPrivateKey, createPublicKey, webcrypto } from "node:crypto";
import { join } from "node:path";

import * as x509 from "@peculiar/x509";

import { readPrivateFileSync, writeFileAtomicSync } from "../storage/atomic-file.js";

// Persistent self-signed TLS identity for the relay's secure device port (8792).
// Pure-JS via @peculiar/x509 + WebCrypto: the HA add-on image has no usable
// openssl (apk add openssl fails), and Node's crypto can generate an EC key but
// not create an X.509 certificate. The identity lives under /data so the pin is
// stable across restarts; the CLI trusts the listener only by SHA-256 SPKI pin,
// never by CA chain, so a pin change forces re-pairing.

const KEY_FILE = "tls-key.pem";
const CERT_FILE = "tls-cert.pem";
const MAX_PEM_BYTES = 16 * 1024;
const ALG = { name: "ECDSA", namedCurve: "P-256", hash: "SHA-256" } as const;

export interface TlsIdentity {
  keyPem: string;
  certPem: string;
  // base64url( SHA-256( DER SubjectPublicKeyInfo ) ) — the value the CLI pins.
  spkiPin: string;
}

// A present-but-inconsistent identity (only one file, an unparseable cert, or a
// key/cert from different generations) is fail-closed like the device registry:
// we never silently overwrite a still-valid key or rotate the pinned identity,
// because that would break every paired client with no server-side explanation.
export class TlsIdentityCorruptError extends Error {}

const crypto = webcrypto as unknown as Crypto;
let providerSet = false;
function ensureProvider(): void {
  if (!providerSet) {
    x509.cryptoProvider.set(crypto);
    providerSet = true;
  }
}

function spkiPinFromDer(spkiDer: ArrayBuffer | Uint8Array): string {
  const buf = Buffer.from(spkiDer instanceof Uint8Array ? spkiDer : new Uint8Array(spkiDer));
  return createHash("sha256").update(buf).digest("base64url");
}

// Loads the persisted identity, or creates and persists a fresh one. A fresh
// identity is only ever generated when BOTH files are absent (a genuine first
// run); any other state is corruption and throws rather than silently rotating
// the pinned key. Concurrent creation is avoided by the caller's startup
// ordering (single relay process).
export async function loadOrCreateTlsIdentity(dataDir: string): Promise<TlsIdentity> {
  ensureProvider();
  const keyPath = join(dataDir, KEY_FILE);
  const certPath = join(dataDir, CERT_FILE);

  const existingKey = readPrivateFileSync(keyPath, MAX_PEM_BYTES);
  const existingCert = readPrivateFileSync(certPath, MAX_PEM_BYTES);

  if (existingKey && existingCert) {
    return await loadExisting(existingKey.toString("utf8"), existingCert.toString("utf8"));
  }
  if (existingKey || existingCert) {
    // Exactly one file present: a partial /data (restore, fsck, tampering).
    // Regenerating here would overwrite a still-valid key and rotate the pin.
    throw new TlsIdentityCorruptError(
      "TLS identity is incomplete (only one of key/cert present); refusing to silently regenerate and break paired clients"
    );
  }

  const identity = await generateIdentity();
  // Write the key first (0600) so the cert never exists without its key.
  writeFileAtomicSync(keyPath, identity.keyPem);
  writeFileAtomicSync(certPath, identity.certPem);
  return identity;
}

// Parses a persisted key+cert, verifying they belong together (the cert's SPKI
// must match the key's public SPKI) so a mixed restore is caught here rather
// than deep inside the TLS stack at handshake time.
async function loadExisting(keyPem: string, certPem: string): Promise<TlsIdentity> {
  let cert: x509.X509Certificate;
  try {
    cert = new x509.X509Certificate(certPem);
  } catch (error) {
    throw new TlsIdentityCorruptError(`stored TLS certificate is unparseable: ${errText(error)}`);
  }

  let keyObject;
  try {
    keyObject = createPrivateKey(keyPem);
  } catch (error) {
    throw new TlsIdentityCorruptError(`stored TLS private key is unparseable: ${errText(error)}`);
  }

  // @peculiar's PublicKey.rawData is the DER SubjectPublicKeyInfo — the exact
  // bytes the pin hashes. Compare it to the key's own public SPKI.
  const certSpki = Buffer.from(cert.publicKey.rawData);
  const keySpki = createPublicKey(keyObject).export({ type: "spki", format: "der" });
  if (!certSpki.equals(keySpki)) {
    throw new TlsIdentityCorruptError("stored TLS key and certificate do not match (mixed restore?)");
  }

  return { keyPem, certPem, spkiPin: spkiPinFromDer(certSpki) };
}

async function generateIdentity(): Promise<TlsIdentity> {
  const keys = await crypto.subtle.generateKey(ALG, true, ["sign", "verify"]);
  const cert = await x509.X509CertificateGenerator.createSelfSigned(
    {
      serialNumber: randomSerial(),
      name: "CN=nova-relay",
      notBefore: new Date("2026-01-01T00:00:00Z"),
      notAfter: new Date("2036-01-01T00:00:00Z"),
      keys,
      signingAlgorithm: ALG,
      extensions: [
        new x509.BasicConstraintsExtension(false, undefined, true),
        new x509.KeyUsagesExtension(
          x509.KeyUsageFlags.digitalSignature | x509.KeyUsageFlags.keyEncipherment,
          true
        ),
        await x509.SubjectKeyIdentifierExtension.create(keys.publicKey),
      ],
    },
    crypto
  );

  const spkiDer = await crypto.subtle.exportKey("spki", keys.publicKey);
  const pkcs8 = Buffer.from(await crypto.subtle.exportKey("pkcs8", keys.privateKey));
  const keyPem = pemWrap("PRIVATE KEY", pkcs8);
  return {
    keyPem,
    certPem: `${cert.toString("pem")}\n`,
    spkiPin: spkiPinFromDer(spkiDer),
  };
}

function pemWrap(label: string, der: Buffer): string {
  const b64 = der.toString("base64").replace(/(.{64})/g, "$1\n");
  return `-----BEGIN ${label}-----\n${b64}\n-----END ${label}-----\n`;
}

function randomSerial(): string {
  return Buffer.from(webcrypto.getRandomValues(new Uint8Array(8))).toString("hex");
}

function errText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
