// SPDX-License-Identifier: AGPL-3.0-only

// Shared by every ranked-count answer on metrics: the ledger's fixed
// columns (MetricsTotals.svelte) and the user-defined widgets in
// lib/topTalkers.svelte.ts.
export function topNBy<T>(items: T[], keyOf: (item: T) => string | undefined, n: number) {
  const counts = new Map<string, number>()
  for (const item of items) {
    const key = keyOf(item)
    if (!key) continue
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, n)
    .map(([label, count]) => ({ label, count }))
}
