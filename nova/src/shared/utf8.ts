// WHATWG TextDecoder accepts one optional leading UTF-8 BOM. `fatal` is the
// important part: Buffer.toString("utf8") would silently replace malformed
// bytes with U+FFFD and could turn a successful write into corrupted data.
export function decodeUtf8Strict(data: Uint8Array): string {
  return new TextDecoder("utf-8", { fatal: true }).decode(data);
}
