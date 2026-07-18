import * as opaque from "@serenity-kit/opaque";
import { beforeAll, describe, expect, it } from "vitest";

import {
  OPAQUE_CLIENT_ID,
  OPAQUE_KSF,
  OPAQUE_SERVER_ID,
  finishLogin,
  opaqueReady,
  registerCode,
  startLogin,
} from "../../nova/src/security/opaque-server.js";

// Exercises the server wrapper against @serenity-kit's own JS client to prove the
// register -> startLogin -> finishLogin plumbing and the fail-closed path. The
// cross-implementation Go interop under these exact params is proven separately
// by the known-answer spike; this keeps the wrapper honest without a Go build.
const IDENTIFIERS = { client: OPAQUE_CLIENT_ID, server: OPAQUE_SERVER_ID };
const KSF = { "argon2id-custom": OPAQUE_KSF } as const;

function jsClientLogin(code: string, reg: ReturnType<typeof registerCode>, userId: string): string | null {
  const { clientLoginState, startLoginRequest } = opaque.client.startLogin({ password: code });
  const started = startLogin(reg, userId, startLoginRequest);
  if (started === null) {
    return null;
  }
  const { serverLoginState, loginResponse } = started;
  const finish = opaque.client.finishLogin({
    clientLoginState,
    loginResponse,
    password: code,
    identifiers: IDENTIFIERS,
    keyStretching: KSF,
  });
  if (!finish) {
    return null;
  }
  const serverSessionKey = finishLogin(serverLoginState, finish.finishLoginRequest);
  if (serverSessionKey === null) {
    return null;
  }
  expect(serverSessionKey).toBe(finish.sessionKey);
  return serverSessionKey;
}

beforeAll(async () => {
  await opaqueReady();
});

describe("opaque-server", () => {
  it("completes a full register/login and both sides agree on the session key", () => {
    const code = "473921";
    const userId = "gen-abc";
    const reg = registerCode(code, userId);
    const key = jsClientLogin(code, reg, userId);
    expect(key).toMatch(/^[A-Za-z0-9_-]+$/);
  });

  it("rejects a wrong code (fail-closed)", () => {
    const reg = registerCode("111111", "gen-1");
    // Wrong password: @serenity's finishLogin returns undefined client-side.
    const { clientLoginState, startLoginRequest } = opaque.client.startLogin({ password: "999999" });
    const started = startLogin(reg, "gen-1", startLoginRequest);
    expect(started).not.toBeNull();
    const finish = opaque.client.finishLogin({
      clientLoginState,
      loginResponse: started!.loginResponse,
      password: "999999",
      identifiers: IDENTIFIERS,
      keyStretching: KSF,
    });
    // Either the client cannot finish, or the server rejects the forged KE3.
    if (finish) {
      expect(finishLogin(started!.serverLoginState, finish.finishLoginRequest)).toBeNull();
    } else {
      expect(finish).toBeUndefined();
    }
  });

  it("returns null (no throw) on a malformed KE1 — symmetric with finishLogin", () => {
    const reg = registerCode("333333", "gen-x");
    for (const bad of ["", "not-base64!!", "AAAA", "x".repeat(10)]) {
      expect(startLogin(reg, "gen-x", bad)).toBeNull();
    }
  });

  it("scopes a record to its userIdentifier (a different generation cannot log in)", () => {
    const reg = registerCode("222222", "gen-A");
    // Correct code, but the server verifies against a DIFFERENT generation id
    // than the record was made for. Binding is enforced at finish, not start:
    // the handshake must not yield an agreed session key.
    const { clientLoginState, startLoginRequest } = opaque.client.startLogin({ password: "222222" });
    const started = startLogin(reg, "gen-B", startLoginRequest);
    expect(started).not.toBeNull();
    const finish = opaque.client.finishLogin({
      clientLoginState,
      loginResponse: started!.loginResponse,
      password: "222222",
      identifiers: IDENTIFIERS,
      keyStretching: KSF,
    });
    if (finish) {
      // Client produced a key, but the server (different generation) must reject.
      expect(finishLogin(started!.serverLoginState, finish.finishLoginRequest)).toBeNull();
    } else {
      expect(finish).toBeUndefined();
    }
  });
});
