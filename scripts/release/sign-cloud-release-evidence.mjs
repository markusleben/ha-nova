#!/usr/bin/env node

import {
  createHash,
  createPrivateKey,
  sign,
} from "node:crypto";
import { readFileSync } from "node:fs";

const [
  version,
  os,
  arch,
  binaryName,
  binaryPath,
  sourceTreeSHA,
  platformList,
] = process.argv.slice(2);

function fail(message) {
  console.error(`[sign-cloud-release-evidence] ERROR: ${message}`);
  process.exit(1);
}

if (
  !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc[1-9]\d*)?$/.test(
    version ?? "",
  )
) {
  fail("version must be strict X.Y.Z or X.Y.Z-rcN");
}
if (!["macos", "linux", "windows"].includes(os)) {
  fail("os must be macos, linux, or windows");
}
if (!["amd64", "arm64"].includes(arch)) {
  fail("arch must be amd64 or arm64");
}
if (!/^[0-9a-f]{40}$/.test(sourceTreeSHA ?? "")) {
  fail("source tree must be a full lowercase SHA-1");
}
const platforms = (platformList ?? "").split(",").filter(Boolean);
if (platforms.length === 0) {
  fail("at least one Cloud platform is required");
}
const privateKeyPEM = process.env.HA_NOVA_CLOUD_RELEASE_SIGNING_KEY_PEM ?? "";
if (privateKeyPEM.length === 0) {
  fail("HA_NOVA_CLOUD_RELEASE_SIGNING_KEY_PEM is required");
}

let privateKey;
try {
  privateKey = createPrivateKey(privateKeyPEM);
} catch {
  fail("release signing key must be a valid private PEM key");
}
if (privateKey.asymmetricKeyType !== "ed25519") {
  fail("release signing key must be Ed25519");
}

const binarySHA256 = createHash("sha256")
  .update(readFileSync(binaryPath))
  .digest("hex");
const payload = {
  schema: 1,
  version,
  os,
  arch,
  binary_name: binaryName,
  binary_sha256: binarySHA256,
  source_tree_sha: sourceTreeSHA,
  platforms,
};
const signature = sign(
  null,
  Buffer.from(JSON.stringify(payload)),
  privateKey,
).toString("base64");

process.stdout.write(
  `${JSON.stringify({
    schema: payload.schema,
    source_tree_sha: sourceTreeSHA,
    binary_sha256: binarySHA256,
    signature,
  })}\n`,
);
