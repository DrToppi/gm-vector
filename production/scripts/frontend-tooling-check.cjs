// Deliberate, temporary defects prove that the required frontend gates fail.
// No production source is changed; exclusive probes are always removed.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { ESLint } = require('../web/node_modules/eslint');

const web = path.resolve(__dirname, '../web');
const probes = [];
async function probe(relative, contents) {
  const filename = path.join(web, relative);
  await fs.writeFile(filename, contents, { flag: 'wx', mode: 0o600 });
  probes.push(filename);
  return filename;
}
function fails(label, executable, args, marker) {
  const result = spawnSync(executable, args, { cwd: web, encoding: 'utf8', timeout: 120000 });
  assert.ifError(result.error);
  assert.equal(result.status, 1, label + ' must exit 1');
  assert.match(result.stdout + result.stderr, marker, label + ' expected failure reason');
}
async function walk(directory) {
  const entries = await fs.readdir(directory, { withFileTypes: true });
  const files = await Promise.all(entries.map(entry => entry.isDirectory()
    ? walk(path.join(directory, entry.name))
    : /\.(ts|tsx)$/.test(entry.name) ? [path.join(directory, entry.name)] : []));
  return files.flat();
}
async function main() {
  const eslint = new ESLint({ cwd: web });
  const files = [...await walk(path.join(web, 'src')), ...await walk(path.join(web, 'tests'))];
  assert(files.length > 20, 'real authored file inventory, not an empty glob');
  for (const filename of files) {
    assert.equal(await eslint.isPathIgnored(filename), false, 'authored source included: ' + path.basename(filename));
    const config = await eslint.calculateConfigForFile(filename);
    assert.equal(config.rules['no-debugger'][0], 2, 'correctness gate enabled for every file');
    assert.equal(config.rules['react-hooks/rules-of-hooks'][0], 2, 'hook correctness enabled');
  }
  const lint = await probe('src/wp03-lint-probe.ts', 'debugger;\nexport {};\n');
  fails('lint', process.execPath, [path.join(web, 'node_modules/eslint/bin/eslint.js'),
    '--max-warnings=0', lint], /no-debugger/);
  const format = await probe('src/wp03-format-probe.ts', 'export const probe={a:1};\n');
  fails('format', process.execPath, [path.join(web, 'node_modules/prettier/bin/prettier.cjs'),
    '--check', format], /Code style issues/);
  const component = await probe('tests/wp03-gate-probe.test.ts',
    "import { it, expect } from 'vitest';\nit('WP03 expected gate failure', () => expect('actual').toBe('expected'));\n");
  fails('component', process.execPath, [path.join(web, 'node_modules/vitest/vitest.mjs'),
    'run', component], /WP03 expected gate failure/);
  console.log('Frontend gate negative controls PASS: authored files covered; lint/format/component each reject a distinct temporary defect.');
}
main().catch(error => { console.error(error.message); process.exitCode = 1; })
  .finally(async () => { for (const filename of probes) await fs.unlink(filename); });
