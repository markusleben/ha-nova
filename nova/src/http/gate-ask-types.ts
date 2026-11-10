import type { GateEndpoint } from "./gate.js";

// The ask route's wire with the worker and the device (docs/work/2026-11-03-relay-typed-gate-spec.md
// → Contract → Wire), shared by gate-asks.ts and the worker client.

export interface GateRequest {
  endpoint: GateEndpoint;
  body: Buffer;
}

// The worker's /ask answer (cli/gate_ask.go): title, body, and button are the
// phone's; items are the page's, one per request, worded only when typed.
export interface AskItem {
  verdict: "pass" | "typed" | "refuse";
  why?: string;
  text?: string;
  detail?: string;
  entity_ids?: string[];
}

export interface PreparedAsk {
  verdict: "pass" | "typed" | "refuse";
  lang?: string;
  title?: string;
  body?: string;
  button?: string;
  items: AskItem[];
}

// A device's linked phone (§3), on the worker's wire (cli/gate_ask.go).
export interface PhoneLink {
  service: string;
  device_id: string;
  user_id: string;
  platform?: string;
}

export interface PhoneText {
  lang: string;
  title: string;
  body: string;
  button: string;
}

// A link ask (POST /gate/v1/phone): the worker words it, asking the phone
// linked now (current) first.
export interface LinkWords {
  words: "link";
  device: string;
  current?: PhoneLink;
}

// The worker's /phone answer: approved names the administrator who tapped.
export type PhoneAnswer = { answer: "approved"; user_id: string } | { answer: "declined" | "rejected" | "none" | "unavailable" };

export type AskState =
  | { state: "waiting"; expires_at: number }
  | { state: "approved"; grant_id: string; until: number }
  | { state: "cancelled" }
  | { state: "expired" }
  | { state: "unavailable" } // its only channel, the phone, can't be asked
  | { state: "linked" }; // a link ask: the device's phone link is set
