const DEFAULT_WS_ALLOWLIST: string[] = [
  "config/area_registry/*",
  "config/floor_registry/*",
  "config/label_registry/*",
  "config/category_registry/*",
  "config/entity_registry/*",
  "config/device_registry/*",
  "trace/*",
  "subscribe_events",
  "subscribe_trigger",
  "get_states",
  "ping"
];

export interface WsAllowlist {
  isAllowed(type: string): boolean;
  assertAllowed(type: string): void;
  listPatterns(): string[];
}

export interface WsAllowlistOptions {
  basePatterns?: string[];
  extraPatterns?: string[];
}

export class WsAllowlistError extends Error {
  public readonly code = "WS_TYPE_NOT_ALLOWED";
  public readonly status = 403;

  public constructor(type: string) {
    super(`WS message type is not allowlisted: ${type}`);
  }
}

export function createWsAllowlist(options: WsAllowlistOptions = {}): WsAllowlist {
  const patterns = [
    ...(options.basePatterns ?? DEFAULT_WS_ALLOWLIST),
    ...(options.extraPatterns ?? [])
  ];

  return {
    isAllowed(type: string): boolean {
      return patterns.some((pattern) => matchesPattern(type, pattern));
    },
    assertAllowed(type: string): void {
      if (!this.isAllowed(type)) {
        throw new WsAllowlistError(type);
      }
    },
    listPatterns(): string[] {
      return [...patterns];
    }
  };
}

function matchesPattern(type: string, pattern: string): boolean {
  if (pattern.endsWith("/*")) {
    const prefix = pattern.slice(0, -1);
    return type.startsWith(prefix);
  }

  return type === pattern;
}
