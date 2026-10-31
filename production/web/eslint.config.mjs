import prettier from "eslint-config-prettier/flat";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

const coreRules = Object.fromEntries(
  [
    "constructor-super",
    "for-direction",
    "getter-return",
    "no-async-promise-executor",
    "no-class-assign",
    "no-compare-neg-zero",
    "no-cond-assign",
    "no-const-assign",
    "no-constant-binary-expression",
    "no-debugger",
    "no-dupe-args",
    "no-dupe-class-members",
    "no-dupe-else-if",
    "no-dupe-keys",
    "no-duplicate-case",
    "no-ex-assign",
    "no-fallthrough",
    "no-func-assign",
    "no-import-assign",
    "no-invalid-regexp",
    "no-irregular-whitespace",
    "no-loss-of-precision",
    "no-new-native-nonconstructor",
    "no-obj-calls",
    "no-self-assign",
    "no-setter-return",
    "no-sparse-arrays",
    "no-this-before-super",
    "no-unexpected-multiline",
    "no-unreachable",
    "no-unsafe-finally",
    "no-unsafe-negation",
    "no-unsafe-optional-chaining",
    "no-useless-backreference",
    "require-yield",
    "use-isnan",
    "valid-typeof",
  ].map((name) => [name, "error"]),
);

export default [
  { ignores: ["dist/**", "node_modules/**", "coverage/**", "src/locales/**"] },
  // Place formatting exclusions first: the explicit correctness inventory below
  // remains authoritative, including no-unexpected-multiline.
  prettier,
  {
    files: [
      "src/**/*.{ts,tsx}",
      "tests/**/*.{ts,tsx}",
      "*.{ts,mts,cts}",
    ],
    languageOptions: {
      parser: tseslint.parser,
      ecmaVersion: "latest",
      sourceType: "module",
      globals: { ...globals.browser, ...globals.es2022 },
    },
    plugins: {
      "@typescript-eslint": tseslint.plugin,
      "react-hooks": reactHooks,
    },
    linterOptions: { reportUnusedDisableDirectives: "error" },
    rules: {
      ...coreRules,
      "@typescript-eslint/no-array-constructor": "error",
      "@typescript-eslint/no-duplicate-enum-values": "error",
      "@typescript-eslint/no-misused-new": "error",
      "@typescript-eslint/no-non-null-asserted-optional-chain": "error",
      "@typescript-eslint/no-unsafe-declaration-merging": "error",
      "@typescript-eslint/no-wrapper-object-types": "error",
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "error",
    },
  },
  {
    files: ["*.{js,mjs,cjs}", "scripts/**/*.{js,mjs,cjs}"],
    languageOptions: {
      ecmaVersion: "latest",
      globals: globals.node,
    },
    linterOptions: { reportUnusedDisableDirectives: "error" },
    rules: coreRules,
  },
];
