#!/usr/bin/env node

import {
  createHash,
  createPublicKey,
  verify,
} from "node:crypto";
import { readFileSync } from "node:fs";

const [
  bundlePath,
  binaryPath,
  version,
  os,
  arch,
  binaryName,
  sourceTreeSHA,
  platformList,
] = process.argv.slice(2);

function fail(message) {
  console.error(`[verify-cloud-release-evidence] ERROR: ${message}`);
  process.exit(1);
}

let bundle;
try {
  bundle = JSON.parse(readFileSync(bundlePath, "utf8"));
} catch {
  fail("bundle metadata must be readable JSON");
}
const platforms = (platformList ?? "").split(",").filter(Boolean);
const evidence = bundle?.cloud_release;
if (
  bundle?.bundle_format_version !== 1 ||
  bundle.version !== version ||
  bundle.os !== os ||
  bundle.arch !== arch ||
  bundle.binary_name !== binaryName ||
  evidence?.schema !== 1 ||
  evidence.source_tree_sha !== sourceTreeSHA ||
  platforms.length === 0
) {
  fail("bundle identity or Cloud release evidence does not match");
}

const binarySHA256 = createHash("sha256")
  .update(readFileSync(binaryPath))
  .digest("hex");
if (evidence.binary_sha256 !== binarySHA256) {
  fail("bundle evidence does not match the candidate binary");
}

let signature;
try {
  signature = Buffer.from(evidence.signature, "base64");
} catch {
  fail("bundle evidence signature is not valid base64");
}
if (
  signature.length !== 64 ||
  signature.toString("base64") !== evidence.signature
) {
  fail("bundle evidence signature is not canonical Ed25519");
}

const payload = {
  schema: evidence.schema,
  version,
  os,
  arch,
  binary_name: binaryName,
  binary_sha256: binarySHA256,
  source_tree_sha: sourceTreeSHA,
  platforms,
};
const publicKey = createPublicKey({
  key: {
    kty: "OKP",
    crv: "Ed25519",
    x: "hzgQeiYLwpJdgL52IfcsIzTfCstcfqbRuoyWmmPkwrQ",
  },
  format: "jwk",
});
if (
  !verify(
    null,
    Buffer.from(JSON.stringify(payload)),
    publicKey,
    signature,
  )
) {
  fail("bundle evidence signature is invalid");
}

console.log(
  `[verify-cloud-release-evidence] OK: ${os}/${arch} ${binarySHA256}`,
);
