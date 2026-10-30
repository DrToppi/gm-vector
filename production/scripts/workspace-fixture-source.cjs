// Test-only instrumentation. Never imported by the shipped application.
const assert = require('node:assert/strict');
const ts = require('../web/node_modules/typescript');

function instrumentWorkspace(source, filename) {
  const tree = ts.createSourceFile(filename, source, ts.ScriptTarget.Latest, true,
    filename.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  assert.equal(tree.parseDiagnostics.length, 0, 'fixture source must parse');
  const targets = tree.statements.filter(node => ts.isFunctionDeclaration(node) &&
    ['Workspace', 'useWorkspaceController'].includes(node.name?.text));
  assert(targets.length <= 1, 'ambiguous workspace instrumentation target');
  if (!targets.length) return { contents: source, count: 0 };
  const body = targets[0].body;
  assert(body, 'workspace fixture requires a function body');
  const names = new Set();
  for (const statement of body.statements) {
    if (ts.isFunctionDeclaration(statement)) names.add(statement.name?.text);
    if (ts.isVariableStatement(statement)) {
      for (const item of statement.declarationList.declarations) {
        if (ts.isIdentifier(item.name)) names.add(item.name.text);
      }
    }
  }
  for (const name of ['refreshLists', 'loadProject', 'refresh']) {
    assert(names.has(name), 'missing workspace fixture symbol: ' + name);
  }
  const returns = body.statements.filter(ts.isReturnStatement);
  assert.equal(returns.length, 1, 'workspace fixture requires one top-level return');
  const offset = returns[0].getStart(tree);
  const injection = '\nwindow.__fixtureRefreshLists=refreshLists;\n' +
    'window.__fixtureLoadProject=()=>loadProject(refresh.projectID);\n';
  return { contents: source.slice(0, offset) + injection + source.slice(offset), count: 1 };
}

function checkInstrumentation() {
  for (const [name, filename] of [['Workspace', 'main.tsx'],
    ['useWorkspaceController', 'useWorkspaceController.ts']]) {
    const source = `function ${name}(){const refresh={projectID:'A'};` +
      'function refreshLists(){} function loadProject(){} return null;}';
    const result = instrumentWorkspace(source, filename);
    assert.equal(result.count, 1);
    assert(result.contents.indexOf('window.__fixtureRefreshLists') <
      result.contents.indexOf('return null'));
    assert.equal(instrumentWorkspace(source.replaceAll(';', ';\n'), filename).count, 1);
    assert.throws(() => instrumentWorkspace(source.replace('const refresh=', 'const other='), filename),
      /missing workspace fixture symbol/);
    assert.throws(() => instrumentWorkspace(source + source, filename), /ambiguous/);
    assert.throws(() => instrumentWorkspace(source.replace('return null;', 'return null;return null;'), filename),
      /one top-level return/);
  }
  assert.equal(instrumentWorkspace('function App(){return null}', 'main.tsx').count, 0);
  assert.throws(() => instrumentWorkspace('function Workspace( {', 'main.tsx'), /must parse/);
}

module.exports = { instrumentWorkspace, checkInstrumentation };
if (require.main === module) {
  checkInstrumentation();
  console.log('Workspace AST instrumentation PASS: formatting/move tolerant; missing/ambiguous targets fail closed.');
}
