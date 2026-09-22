#!/usr/bin/env node
/**
 * The initial bundle, measured the way a browser pays for it.
 *
 * The NFRs budget the login-to-shell bundle at 200 KB gzipped and make that
 * binding from Phase 0, which is now. So this measures exactly what a cold
 * visit downloads before anything is interactive: every stylesheet and script
 * that index.html references, plus everything it preloads, gzipped, summed.
 *
 * Deliberately not "the size of dist". That directory also holds source maps,
 * which are several times larger than the code and which a browser fetches
 * only when devtools are open. Counting them would report a number nobody
 * experiences and would make the budget meaningless.
 */

import { gzipSync } from "node:zlib";
import { readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const DIST = resolve(HERE, "../dist");

/** Gzipped kilobytes, binding from Phase 0. See docs/roadmap/nfr §1.4. */
const BUDGET_KB = 200;

const html = read(join(DIST, "index.html"));

// Everything the document pulls in before it can run: scripts, stylesheets,
// and modulepreload hints, which are requests too.
const referenced = [
  ...html.matchAll(/<script[^>]+src="\/?([^"]+)"/g),
  ...html.matchAll(/<link[^>]+rel="(?:stylesheet|modulepreload)"[^>]+href="\/?([^"]+)"/g),
  ...html.matchAll(/<link[^>]+href="\/?([^"]+)"[^>]+rel="(?:stylesheet|modulepreload)"/g),
].map((match) => match[1]);

const assets = [...new Set(referenced)].sort();

if (assets.length === 0) {
  fail("index.html references no scripts or stylesheets; has the build run?");
}

let total = 0;
const rows = [];

// index.html itself is a request, and a small one, but it counts.
for (const path of ["index.html", ...assets]) {
  const file = join(DIST, path);

  if (!exists(file)) fail(`index.html references ${path}, which does not exist`);

  const gzipped = gzipSync(readFileSync(file)).length;

  total += gzipped;
  rows.push({ path, raw: statSync(file).size, gzipped });
}

const totalKB = total / 1024;

for (const row of rows.sort((a, b) => b.gzipped - a.gzipped)) {
  console.log(
    `  ${kb(row.gzipped).padStart(9)} gzipped  ${kb(row.raw).padStart(9)} raw   ${row.path}`,
  );
}

console.log(
  `\n  ${kb(total).padStart(9)} gzipped total, against a ${BUDGET_KB} KB budget ` +
    `(${percent(totalKB / BUDGET_KB)} used)`,
);

if (totalKB > BUDGET_KB) {
  fail(
    `the initial bundle is ${kb(total)} gzipped, over the ${BUDGET_KB} KB budget ` +
      `in docs/roadmap/non-functional-requirements.md.\n\n` +
      `Raising the number is a product decision, not a build fix. Either split ` +
      `the offending route out with a dynamic import, or change the budget in ` +
      `the NFRs and say why in the same commit.`,
  );
}

// A budget that is nearly spent is worth knowing about before it is spent.
if (totalKB > BUDGET_KB * 0.9) {
  console.log(
    `\n  Warning: ${percent(totalKB / BUDGET_KB)} of the budget is gone and the ` +
      `application is still Phase 0. The next feature that adds a dependency ` +
      `will break this.`,
  );
}

function kb(bytes) {
  return `${(bytes / 1024).toFixed(1)} KB`;
}

function percent(ratio) {
  return `${(ratio * 100).toFixed(0)}%`;
}

function read(path) {
  if (!exists(path)) fail(`${path} does not exist; run \`make web-build\` first`);

  return readFileSync(path, "utf8");
}

function exists(path) {
  try {
    statSync(path);

    return true;
  } catch {
    return false;
  }
}

function fail(message) {
  console.error(`\nbundle-size: ${message}\n`);
  process.exit(1);
}
