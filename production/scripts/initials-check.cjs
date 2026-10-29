// Pure display contract checks; no credentials, network or production data.
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict');
const ts = require('../web/node_modules/typescript');
const source = fs.readFileSync(require.resolve('../web/src/initials.ts'), 'utf8');
const compiled = ts.transpileModule(source, {compilerOptions: {module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022}}).outputText;
const sandbox = {exports: {}};
vm.runInNewContext(compiled, sandbox);
const {initials} = sandbox.exports;
for (const [name, expected] of [
  ['Тестов Макет', 'МТ'],
  ['Тестов Макет Образцович', 'МТ'],
  ['  тестов\t\nмакет  ', 'МТ'],
  ['Тестов\u00a0\u2003Макет', 'МТ'],
  ['Тест-Дефис Макет', 'МТ'],
  ['Тестов Макет-Пример', 'МТ'],
  ['Администратор', 'А'],
  ['И\u0306тест Е\u0308тест', 'ЁЙ'],
  ['𐐀surname 𐐨given', '𐐀𐐀'],
  ['कुमार किरण', 'किकु'],
  ['', '?'], [' \t\n\u00a0', '?'], [undefined, '?'], [null, '?'],
]) assert.equal(initials(name), expected, `initials for ${JSON.stringify(name)}`);
console.log('Avatar initials: given/family order, patronymic omission, whitespace, Unicode graphemes and missing names passed.');
