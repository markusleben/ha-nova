import {
  OPAQUE_CLIENT_ID,
  OPAQUE_KSF,
  OPAQUE_SERVER_ID,
} from "../../nova/src/security/opaque-server.js";

export const IDENTIFIERS = {
  client: OPAQUE_CLIENT_ID,
  server: OPAQUE_SERVER_ID,
};
export const KSF = { "argon2id-custom": OPAQUE_KSF } as const;
export const RELAY_ID = "hanova-relay-v1.AAAAAAAAAAAAAAAAAAAAAA";
export const RELAY_VERSION = "0.8.0";
export const META = {
  name: "MacBook",
  platform: "darwin",
  client: "codex",
  client_install_id: "install-cloud-1",
};
