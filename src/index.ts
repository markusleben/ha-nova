export interface App {
  version: string;
}

export function createApp(): App {
  return {
    version: "0.1.0"
  };
}
