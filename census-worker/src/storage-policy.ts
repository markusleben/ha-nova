export const MAX_INSTALLATIONS = 20_000;
export const MAX_NEW_INSTALLATIONS_PER_DAY = 500;
export const MAX_BREAKDOWN_ROWS = 20;

const DAY_MS = 86_400_000;
interface CensusSQL {
  exec(
    query: string,
    ...bindings: (string | number | null)[]
  ): { toArray(): unknown[] };
}

export function ensureAdmissionTable(sql: CensusSQL): void {
  sql.exec(
    `CREATE TABLE IF NOT EXISTS admission_days (
       day INTEGER PRIMARY KEY,
       count INTEGER NOT NULL,
       rejected INTEGER NOT NULL DEFAULT 0
     )`,
  );
}

export function admitNewInstallation(
  sql: CensusSQL,
  observedAt: number,
): boolean {
  const day = Math.floor(observedAt / DAY_MS);
  sql.exec("DELETE FROM admission_days WHERE day < ?", day - 1);
  const rows = sql
    .exec("SELECT count FROM admission_days WHERE day = ?", day)
    .toArray() as unknown as { count: number }[];
  if ((rows[0]?.count ?? 0) >= MAX_NEW_INSTALLATIONS_PER_DAY) {
    sql.exec(
      "UPDATE admission_days SET rejected = rejected + 1 WHERE day = ?",
      day,
    );
    return false;
  }
  sql.exec(
    `INSERT INTO admission_days (day, count)
     VALUES (?, 1)
     ON CONFLICT (day) DO UPDATE SET count = count + 1`,
    day,
  );
  return true;
}

export function admissionRejections(
  sql: CensusSQL,
  observedAt: number,
): number {
  const day = Math.floor(observedAt / DAY_MS);
  const rows = sql
    .exec("SELECT rejected FROM admission_days WHERE day = ?", day)
    .toArray() as unknown as { rejected: number }[];
  return rows[0]?.rejected ?? 0;
}

export function boundedNumberRecord(
  rows: Record<string, unknown>[],
  key: string,
  expectedTotal: number,
): Record<string, number> {
  const result: Record<string, number> = {};
  let represented = 0;
  for (const row of rows) {
    const label = row[key];
    const count = row["count"];
    if (typeof label === "string" && typeof count === "number") {
      result[label] = count;
      represented += count;
    }
  }
  const other = expectedTotal - represented;
  if (other > 0) {
    result["other"] = other;
  }
  return result;
}
