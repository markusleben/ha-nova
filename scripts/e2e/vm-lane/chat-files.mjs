// The App's own chat bridge refuses a file the Relay never serves, naming the
// real cause (0.29.0 escape: a chat app's delete under /config/www surfaced
// as a fake sign-in error; fixed by #1072). One Manage everything request
// through Home Assistant's chat-app endpoint (/api/ha_nova/mcp, the path
// claude.ai and ChatGPT take, as the lane's owner token) into the bridge
// inside the NOVA App: a delete of a file in custom_components (www's
// media became deletable in 0.30) must end REFUSED with
// FILE_PATH_DENIED. Nothing is sent to the file: the plan stops at its read.
// Needs, once on the throwaway VM: chat apps on (its NOVA page), the setup
// token's user at Manage everything, and the App's file_access read or
// readwrite; without them the row FAILs saying so.
const WRONG_CAUSE = /sign[- ]?in|credential|ha-nova setup|file access (setting|option)|turn on file access|can't reach (its|the) (NOVA )?Relay/i;

export async function chatFileRefusal({ ha, llat, app, sup, check }) {
  const name = "chat app (bridge in the App): a file in custom_components is refused, naming why";
  const access = (await sup(`/addons/${app}/info`))?.options?.file_access;
  if (access !== "read" && access !== "readwrite") return check("p", name, false, `the App's file_access is ${access ?? "unknown"}; set read or readwrite on the throwaway VM's NOVA Relay App once, then rerun`);
  const res = await fetch(`${ha}/api/ha_nova/mcp`, { method: "POST", signal: AbortSignal.timeout(60_000),
    headers: { authorization: `Bearer ${llat}`, "content-type": "application/json", accept: "application/json, text/event-stream" },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "ha_plan", arguments: { kind: "raw",
      requests: [{ endpoint: "files", payload: { action: "delete_file", path: "/config/custom_components/vm-lane/camera.py" } }] } } }) });
  if (res.status === 404) return check("p", name, false, "chat apps are off on the throwaway VM: turn them on once on its NOVA page, then rerun");
  const line = (await res.text()).split("\n").map((l) => l.replace(/^data: /, "")).find((l) => l.startsWith("{")) ?? "{}";
  const text = JSON.parse(line).result?.content?.[0]?.text ?? line;
  let d = {};
  try { d = JSON.parse(text); } catch { /* the detail shows the raw answer */ }
  const words = [d.error?.code, d.error?.message, d.error?.next_step, d.next_step].filter(Boolean).join(" | ") || text;
  return check("p", name, d.error?.code === "REFUSED" && words.includes("FILE_PATH_DENIED") && !WRONG_CAUSE.test(words), `HTTP ${res.status}: ${words.slice(0, 200)}`);
}
