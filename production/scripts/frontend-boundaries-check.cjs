const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const ts = require('../web/node_modules/typescript');
const root = path.resolve(__dirname, '../web/src');
const files = fs.readdirSync(root).filter(name => /\.(ts|tsx)$/.test(name));
const graph = new Map(files.map(name => [name, []]));
for (const filename of files) {
  const tree = ts.createSourceFile(filename, fs.readFileSync(path.join(root, filename), 'utf8'),
    ts.ScriptTarget.Latest, true, filename.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  assert.equal(tree.parseDiagnostics.length, 0, 'module parses: ' + filename);
  for (const node of tree.statements) {
    if (!ts.isImportDeclaration(node) && !ts.isExportDeclaration(node)) continue;
    const specifier = node.moduleSpecifier?.text;
    if (!specifier?.startsWith('./') || node.isTypeOnly || node.importClause?.isTypeOnly) continue;
    const bindings = node.importClause?.namedBindings;
    if (bindings && ts.isNamedImports(bindings) && !node.importClause.name &&
      bindings.elements.length && bindings.elements.every(item => item.isTypeOnly)) continue;
    if (node.exportClause && ts.isNamedExports(node.exportClause) &&
      node.exportClause.elements.length && node.exportClause.elements.every(item => item.isTypeOnly)) continue;
    const target = [specifier.slice(2) + '.ts', specifier.slice(2) + '.tsx'].find(name => graph.has(name));
    if (target) graph.get(filename).push(target);
  }
}
const finished = new Set(), active = [];
function visit(filename) {
  assert(!active.includes(filename), 'runtime import cycle: ' + [...active, filename].join(' → '));
  if (finished.has(filename)) return;
  active.push(filename);
  for (const target of graph.get(filename)) visit(target);
  active.pop(); finished.add(filename);
}
for (const filename of files) visit(filename);
// Public compatibility barrel must not become an implementation dependency.
const leaves = ['Modal.tsx', 'TaskEditor.tsx', 'LinkEditor.tsx', 'ProjectEditor.tsx',
  'PeoplePanel.tsx', 'ActivityEditor.tsx', 'DirectorySearch.tsx', 'ParticipationDetails.tsx',
  'participation.ts', 'Workspace.tsx', 'useWorkspaceController.ts', 'CreateProject.tsx'];
for (const filename of leaves) {
  assert(graph.has(filename), 'approved extracted module exists: ' + filename);
  if (filename !== 'Workspace.tsx' && filename !== 'useWorkspaceController.ts') {
    assert(!graph.get(filename).includes('Editors.tsx'), 'leaf imports compatibility barrel: ' + filename);
  }
}
assert(graph.get('Workspace.tsx').includes('useWorkspaceController.ts'), 'view consumes workspace controller');
assert(!graph.get('Workspace.tsx').includes('api.ts'), 'view does not own request/session mutations');
assert(graph.get('main.tsx').includes('Workspace.tsx'), 'entry keeps bootstrap and imports workspace view');
console.log('Frontend boundaries PASS: ' + files.length + ' authored modules; no runtime cycle; approved leaf/controller seams retained.');
