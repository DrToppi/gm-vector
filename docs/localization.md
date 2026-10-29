# Languages and localization

gmVector includes English (`en`), Russian (`ru`), German (`de`), French (`fr`), Spanish (`es`), Brazilian Portuguese (`pt-BR`) and Simplified Chinese (`zh-Hans`). English is the fallback language. These translations were prepared with AI assistance; they have not been reviewed by native-language professional translators.

The language selector is available before and after sign-in. Before sign-in, the selected language is stored only in this browser (`gm-vector.locale`); a new browser starts in English. After sign-in, the account preference takes precedence. Changing language saves the preference through `PUT /api/v1/auth/preferences` using the authenticated cookie session and CSRF protection. Existing accounts default to Russian. Language changes do not remount the workspace or discard unsaved task forms.

## Coverage

The seven catalogs cover authentication and TOTP enrollment, project and task forms, effort and participation settings, administration, directory-source selection, comments and delivery states, the Gantt chart, SVG mind map, encrypted Bug Report flow, public GitHub issue drafts, and HTML/CSV/print-PDF report headings. Dates in timestamps use the selected locale. User-entered names, titles, descriptions and comments are not translated, and are never sent to a translation service.

API identifiers, status values, integration scopes and audit-event identifiers remain stable machine values. The currently supported business calendar and Moscow scheduling timezone do not change with the interface language. ISO date inputs and exported data preserve their machine-readable representation. JSON exports preserve the import contract rather than translating field names.

Server responses use stable error codes. Common validation, authorization, conflict and rate-limit failures have localized client explanations. Error messages include the API error code and a bounded request reference for diagnosis. Detailed legacy server validation messages remain available in Russian; other languages currently use a general localized explanation rather than translating arbitrary server text. Mail templates are server-owned and are outside the browser catalog coverage. Imported calendar titles and other provider-supplied content are not translated.

## Implementation

The browser uses pinned `i18next` and `react-i18next`. Catalogs are in `production/web/src/locales/<locale>.json`; `i18n.ts` handles initialization, locale allowlisting and persistence. Existing extracted messages use stable `m`-prefixed source IDs. New messages may use descriptive semantic keys. Do not rename an ID merely because its English wording changes. Inspect references to an ID to understand context before editing it.

All UI messages are rendered through React or SVG text nodes. There is no post-render DOM text replacement. React escapes interpolated user data; exported HTML applies its own escaping. Translation catalogs must contain plain text, not HTML. Global status maps use dynamic getters, and export headings are evaluated when the export runs, so neither becomes stuck in the startup language.

Plural messages use `count` and i18next JSON-v4 suffixes, including Russian `one/few/many/other`. Each locale supplies every plural category required by `Intl.PluralRules`. Interpolation names must match the English catalog exactly.

All catalogs ship with the browser bundle. This permits immediate switching without fetching translations and avoids a translation-network dependency. It increases the initial JavaScript payload; the current build emits Vite's advisory about a chunk over 500 kB before compression. Language splitting can be considered when the catalog grows.

## Verification and contribution

From `production/web`, run:

```sh
npm ci
npm run test:i18n
npm run check
npm run build
```

The localization test checks all seven catalogs for key parity, nonempty values, interpolation parity, plural coverage and accidental markup. It also checks literal UI references, dynamic task-status changes, all-language HTML/CSV headings, and HTML escaping with synthetic hostile text. The same suite checks that GitHub draft URLs contain only explicitly supplied issue fields and an allowed software version, and that oversized drafts trigger the manual-copy fallback.

For manual browser review, switch languages before login, sign in, change the account language, reload, and verify persistence. Open Gantt, mind map, task, participation and Bug Report views. Check labels, keyboard access, text wrapping, and that task data and unsaved edits stay unchanged. Review plural counts such as 1, 2, 5 and 21. Use synthetic data for this review.

Translation corrections are welcome through reviewed contributions. Include the locale, key, UI context, proposed wording and any screenshot made with synthetic data. Do not attach production task content or personal information.

The isolated browser E2E suite additionally verifies all seven first-run language headings, saving the authenticated profile language, persistence after reload, and the public-issue preview/consent boundary. These checks use synthetic accounts and data, not a production installation. Automated coverage does not replace native-speaker review or visual review at every screen size.
