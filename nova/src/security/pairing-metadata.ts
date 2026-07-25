export const MAX_METADATA_BYTES = 4 * 1024;

export interface DeviceMetadata {
  name: string;
  platform: string;
  client: string;
  clientInstallId: string;
}

export function decodePairingMetadata(
  plaintext: Buffer | null,
): DeviceMetadata | null {
  if (plaintext === null || plaintext.length > MAX_METADATA_BYTES) {
    return null;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(plaintext.toString("utf8"));
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) {
    return null;
  }
  const metadata = parsed as Record<string, unknown>;
  const name = boundedString(metadata.name, 64);
  const platform = boundedString(metadata.platform, 32);
  const client = boundedString(metadata.client, 32);
  const clientInstallId = boundedString(metadata.client_install_id, 128);
  if (
    name === null ||
    platform === null ||
    client === null ||
    clientInstallId === null
  ) {
    return null;
  }
  return { name, platform, client, clientInstallId };
}

function boundedString(value: unknown, maxLength: number): string | null {
  if (
    typeof value !== "string" ||
    value.length === 0 ||
    value.length > maxLength
  ) {
    return null;
  }
  if (/[\u0000-\u001f\u007f]/.test(value)) {
    return null;
  }
  return value;
}
