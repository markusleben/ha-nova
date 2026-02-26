import { startBridge } from "./start.js";

startBridge().catch((error: unknown) => {
  const message = error instanceof Error ? error.message : "Unknown startup error";
  console.error(JSON.stringify({ level: "error", message: "Bridge failed to start", context: { error: message } }));
  process.exit(1);
});
