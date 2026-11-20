import type { Stats } from "node:fs";
import { lstat } from "node:fs/promises";
import { join } from "node:path";

// lstat, not stat: a leaf symlink must neither leak outside metadata into list
// results nor count its target's size toward the caps; a leaf deleted since
// readdir is simply gone. Eight reads at a time; none starts after a failure.
export async function lstatLeaves(dir: string, names: string[]): Promise<(Stats | null)[]> {
  const infos: (Stats | null)[] = names.map(() => null);
  let next = 0;
  let failure: { error: unknown } | undefined;
  const worker = async (): Promise<void> => {
    while (!failure && next < names.length) {
      const i = next++;
      infos[i] = await lstat(join(dir, names[i]!)).catch((error: unknown) => {
        failure ??= isEnoent(error) ? undefined : { error };
        return null;
      });
    }
  };
  await Promise.all(Array.from({ length: Math.min(8, names.length) }, worker));
  if (failure) {
    throw failure.error;
  }
  return infos;
}

export function isEnoent(error: unknown): boolean {
  return Boolean(error && typeof error === "object" && (error as { code?: string }).code === "ENOENT");
}
