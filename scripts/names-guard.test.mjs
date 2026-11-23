import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, it, test } from "node:test";

/*
 * #1456: the owner's real name stays out of the tree. Only the sha256 of each
 * guarded word (lower case, unsalted, as orbit's no-real-names guard) is
 * stored, so this file does not itself spell the name. The shared
 * private-details CI job covers new commits; this scans every tracked text
 * file. Hits are reported as path:line, never as the word.
 *
 * MV_NAMES_GUARD_ROOT points the scan at another directory (a plain file
 * tree, no git needed), which the planted-value proof in the commit uses.
 */
const root = process.env.MV_NAMES_GUARD_ROOT ?? path.resolve(import.meta.dirname, "..");

/** sha256 of each guarded word, lower case. */
const BANNED = new Set([
  // the owner's first name, #1456
  "e1608f75c5d7813f3d4031cb30bfb786507d98137538ff8e128a6ff74e84e643",
  // the owner's surname, #1456
  "4aa59b5a3761c43828cf2c0f44a385d94d078934936482f66e573cb444e7e481",
]);

/** Files never scanned: generated lockfile (hashes, no prose) and binaries by extension. */
const SKIP = new Set(["frontend/package-lock.json"]);

/** @type {Map<string, boolean>} */
const seen = new Map();
/** @param {string} word */
export function banned(word) {
  let hit = seen.get(word);
  if (hit === undefined) {
    hit = BANNED.has(createHash("sha256").update(word).digest("hex"));
    seen.set(word, hit);
  }
  return hit;
}

/**
 * The lower-case words a line could spell a guarded word with: camelCase runs
 * split, `_`, `-`, `.`, `/` and digits as separators, and each token also
 * tried without a trailing `s` or `es` (plurals, possessives).
 * @param {string} line
 */
export function wordsOf(line) {
  /** @type {Set<string>} */
  const words = new Set();
  const spaced = line.replace(/([a-z0-9])([A-Z])/g, "$1 $2").replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2").toLowerCase();
  for (const token of [...(spaced.match(/[a-z0-9]+/g) ?? []), ...(spaced.match(/[a-z]+/g) ?? [])]) {
    words.add(token);
    if (token.length <= 2) continue;
    if (token.endsWith("s")) words.add(token.slice(0, -1));
    if (token.endsWith("es")) words.add(token.slice(0, -2));
  }
  return words;
}

function trackedTextFiles() {
  const names = process.env.MV_NAMES_GUARD_ROOT
    ? execFileSync("find", [".", "-type", "f", "-print0"], { cwd: root, encoding: "utf8" }).split("\0").filter(Boolean).map((n) => n.replace(/^\.\//, ""))
    : execFileSync("git", ["ls-files", "-z"], { cwd: root, encoding: "utf8", maxBuffer: 1 << 28 }).split("\0").filter(Boolean);
  /** @type {{ name: string, text: string }[]} */
  const files = [];
  for (const name of names) {
    if (SKIP.has(name)) continue;
    let data;
    try {
      data = readFileSync(path.join(root, name));
    } catch {
      continue; // deleted in the working tree, or a submodule entry
    }
    if (data.subarray(0, 8192).includes(0)) continue; // binary
    files.push({ name, text: data.toString("utf8") });
  }
  return files;
}

describe("no real names (#1456)", () => {
  test("no tracked text file contains a guarded word", { timeout: 60_000 }, () => {
    /** @type {string[]} */
    const hits = [];
    for (const { name, text } of trackedTextFiles()) {
      const lines = text.split("\n");
      for (let i = 0; i < lines.length; i++) {
        for (const token of wordsOf(lines[i])) {
          if (banned(token)) {
            hits.push(`${name}:${i + 1}`);
            break;
          }
        }
      }
    }
    assert.deepEqual(hits, [], "use an invented name instead (#1456)");
  });
});

describe("the guard's matcher (#1456)", () => {
  /** @param {Set<string>} got @param {string[]} want */
  const includesAll = (got, want) => {
    for (const word of want) assert.ok(got.has(word), `${word} missing from ${JSON.stringify([...got])}`);
  };

  it("finds a word inside kebab, snake, camel and path forms", () => {
    includesAll(wordsOf("laptop-brightwell"), ["brightwell"]);
    includesAll(wordsOf("BRIGHTWELL_ITEMS"), ["brightwell", "items"]);
    includesAll(wordsOf("const brightwellItems = []"), ["brightwell", "items"]);
    includesAll(wordsOf("/home/brightwell/x"), ["brightwell"]);
    includesAll(wordsOf("brightwell2"), ["brightwell"]);
  });

  it("finds plural and possessive forms", () => {
    includesAll(wordsOf("the Brightwells' boiler"), ["brightwell"]);
  });

  it("does not join separate words", () => {
    assert.equal(wordsOf("bright well").has("brightwell"), false);
  });
});
