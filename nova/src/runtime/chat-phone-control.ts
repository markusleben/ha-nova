import { request as httpRequest } from "node:http";

// The App's client for the bridge's control channel (contract
// docs/work/2026-10-19-chat-bridge-contract.md, "Control channel (0.28)"),
// the only way the App changes a chat-app phone: loopback only, with the
// control secret that Home Assistant never holds. `override` is the NOVA
// page's click alone; the machine API never sends it.

export const CONTROL_PORT = 8795;

export interface ChatPhone {
  device_id: string;
  name: string;
  platform: string; // ios, android, or "" (unknown)
}

export interface ChatPhoneLink extends ChatPhone {
  state: "ready" | "gone" | "unavailable";
  why: string;
}

export interface ChatPhones {
  phones: ChatPhone[]; // the user's own phones that can confirm, the linked one left out
  link: ChatPhoneLink | null;
}

// From the 202 on: the step running (at the end, the last one), the phone
// it asks, and a move's new phone.
export interface ChatPhoneJob {
  status: string; // running, done, declined, timeout, rejected, changed, failed, lost
  step?: "old" | "new";
  phone?: string;
  platform?: string;
  to?: string;
}

export interface AdoptPhone {
  service: string;
  device_id: string;
  name: string;
}

export type ChatPhoneChange =
  | { action: "adopt"; phone: AdoptPhone }
  | { action: "move"; phone: { device_id: string }; override?: true }
  | { action: "stop"; override?: true };

// The bridge's refusal (409 PHONE_EXISTS, ...); 503 CHAT_PHONE_UNAVAILABLE
// when it can't be reached or read (nothing changed); 504 CHAT_PHONE_UNKNOWN
// when a change went out and its answer was lost (it may have happened).
export class ChatPhoneError extends Error {
  constructor(readonly status: number, readonly code: string, message: string) {
    super(message);
  }
}

export interface ChatPhoneControl {
  phones(user: string): Promise<ChatPhones>;
  start(user: string, change: ChatPhoneChange): Promise<string | null>; // a move, or a stop that asks a phone: its job id
  job(id: string): Promise<ChatPhoneJob>;
}

const JOB = /^[0-9a-f]{32}$/;

const unreadable = (): ChatPhoneError => new ChatPhoneError(503, "CHAT_PHONE_UNAVAILABLE", "the chat-app bridge's answer can't be read");

const named = (p: unknown): boolean => typeof p === "object" && p !== null && typeof (p as ChatPhone).device_id === "string" && typeof (p as ChatPhone).name === "string";

// secret: the control secret, null while chat apps are off.
export function createChatPhoneControl(secret: () => string | null, port = CONTROL_PORT): ChatPhoneControl {
  const call = (method: "GET" | "POST", path: string, body?: unknown): Promise<Record<string, unknown>> =>
    new Promise((resolve, reject) => {
      const unavailable = (why: string): ChatPhoneError => new ChatPhoneError(503, "CHAT_PHONE_UNAVAILABLE", `the chat-app bridge can't be reached (${why})`);
      // A change that went out may have happened; a read changed nothing.
      const lost = (why: string): ChatPhoneError =>
        method === "POST" ? new ChatPhoneError(504, "CHAT_PHONE_UNKNOWN", `the chat-app bridge's answer was lost (${why}); the change may have happened`) : unavailable(why);
      const key = secret();
      if (key === null) {
        reject(unavailable("chat apps are off"));
        return;
      }
      const raw = body === undefined ? undefined : JSON.stringify(body);
      const headers = { authorization: `Bearer ${key}`, ...(raw === undefined ? {} : { "content-type": "application/json", "content-length": Buffer.byteLength(raw) }) };
      // A change waits for the bridge's lock, which a chat app's call holds while it reads or writes Home Assistant.
      const timeout = method === "POST" ? 30_000 : 10_000;
      const req = httpRequest({ host: "127.0.0.1", port, method, path: `/control/v1/${path}`, headers, timeout }, (res) => {
        const chunks: Buffer[] = [];
        let size = 0;
        res.on("data", (chunk: Buffer) => {
          size += chunk.length;
          if (size > 64 * 1024) {
            req.destroy(new Error("its answer is too large"));
          } else {
            chunks.push(chunk);
          }
        });
        res.on("error", (error) => reject(lost(error.message)));
        res.on("close", () => {
          if (!res.complete) {
            reject(lost("its answer was cut off"));
          }
        });
        res.on("end", () => {
          let answer: Record<string, unknown> = {};
          try {
            const parsed: unknown = JSON.parse(Buffer.concat(chunks).toString("utf8"));
            answer = typeof parsed === "object" && parsed !== null ? (parsed as Record<string, unknown>) : {};
          } catch {
            // a non-JSON answer is a refusal below
          }
          const status = res.statusCode ?? 0;
          if (status === 200 || status === 202) {
            resolve(answer);
            return;
          }
          const error = (answer.error ?? {}) as { code?: unknown; message?: unknown };
          reject(new ChatPhoneError(status, typeof error.code === "string" ? error.code : "CHAT_PHONE_FAILED", typeof error.message === "string" ? error.message : `the bridge answered ${status}`));
        });
      });
      req.on("timeout", () => req.destroy(new Error("no answer in time")));
      req.on("error", (error) => reject((error as NodeJS.ErrnoException).code === "ECONNREFUSED" ? unavailable(error.message) : lost(error.message)));
      req.end(raw);
    });
  return {
    phones: async (user) => {
      const answer = await call("GET", `phones?user=${encodeURIComponent(user)}`);
      if (!Array.isArray(answer.phones) || !answer.phones.every(named) || (answer.link !== null && !named(answer.link))) {
        throw unreadable();
      }
      return answer as unknown as ChatPhones;
    },
    start: async (user, change) => {
      const answer = await call("POST", "phone", { user, ...change });
      if (answer.job === undefined) {
        return null;
      }
      if (typeof answer.job !== "string" || !JOB.test(answer.job)) {
        throw new ChatPhoneError(504, "CHAT_PHONE_UNKNOWN", "the chat-app bridge started a change without a readable job");
      }
      return answer.job;
    },
    job: async (id) => {
      const answer = await call("GET", `phone/${encodeURIComponent(id)}`);
      if (typeof answer.status !== "string") {
        throw unreadable();
      }
      return answer as unknown as ChatPhoneJob;
    },
  };
}
