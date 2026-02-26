import { startRelay } from "./start.js";

startRelay().catch((error: unknown) => {
  const message = error instanceof Error ? error.message : "Unknown startup error";
  console.error(JSON.stringify({ level: "error", message: "Relay failed to start", context: { error: message } }));
  process.exit(1);
});
