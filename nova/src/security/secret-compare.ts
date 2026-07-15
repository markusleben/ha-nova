import { createHash, timingSafeEqual } from "node:crypto";

export function constantTimeEqualSecret(left: string, right: string): boolean {
  // Fixed-size digests avoid leaking the secret length through an early return.
  const leftDigest = createHash("sha256").update(left).digest();
  const rightDigest = createHash("sha256").update(right).digest();
  return timingSafeEqual(leftDigest, rightDigest);
}
