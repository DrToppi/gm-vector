# WP-05 WebKit modal focus compatibility

Status: **ACTUAL RED; BOUNDED FIX PLAN INDEPENDENTLY ADMITTED**. 2026-11-03.
The reviewer checked actual private control evidence, existing Modal and WAI APG.
Implementation must address initial guard/autofocus and positive-tabindex ordering;
this is plan admission, not verification of a focus correction.

The required native Tab oracle passed Chromium/Firefox but failed pinned WebKit
in a real task editor. A separate diagnostic preserved the unchanged assertion:
multiple button/select/checkbox/date/summary stops were visited, then Tab after
the final textarea moved activeElement to BODY and document.hasFocus became false
while dialog[open] remained present. Date inputs had native internal tab stops.
This is not evidence of background-page editing or text-fields-only keyboard mode.

Diagnostic evidence SHA-256:
`7d9e3243cb322107ba596b0c4c03cb3a6473630d05afffd09a31ef9cd87eb93b`;
private process log SHA-256:
`6ef43ca420234e3d4e85a55d1224a7beae62715bb23aa6002a6ed84f643f72b2`.
The single-engine run deliberately failed the full three-engine completeness guard
and is not full acceptance. No runtime focus correction preceded this RED.

## Intended behaviour and bounded correction

Preserve the accepted modal Tab/Shift-Tab containment contract, native dialog
modality, focus restoration, visible focus, draft state and existing cancel/
backdrop/cross rules. [WAI-ARIA's modal pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/)
specifies cycling at the first/last tabbable control. Native implementation
differences do not authorize weakening the existing oracle.

Root owns only production/web/src/Modal.tsx and a new tests/modal-focus.test.tsx
for this correction. No dependency, API/auth, locale text or broad styling change.
Use small leading/trailing focus guards inside the native dialog to wrap only
when native traversal reaches a boundary. Keep intermediate Tab behaviour native,
including composite date-input stops; do not intercept every Tab by host identity.
Guard focus immediately moves to the appropriate visible, enabled tabbable control
and cannot remain as an invisible keyboard stop. Never focus the background page.

Determine current candidates at the time of boundary focus, excluding guards,
hidden/disabled/inert/negative-tabindex and closed-details content, respecting
positive tabindex ordering if present. Preserve autofocus precedence and native
showModal/close timing. No timers, global focus stealing, permanent key suppression,
browser preference/CSP bypass or test forcefocus to manufacture green evidence.

## Verification

Component tests cover guard wrapping, dynamic disabled/hidden controls, direction,
input/draft preservation, initial/autofocus compatibility and close semantics.
DOM tests are boundary-handler evidence, not native keyboard proof.
Actual unchanged WebKit native input must pass the original assertion after the
fix; Chromium/Firefox must retain Tab/Shift-Tab, visible focus, native date input,
Escape/cross, menu and focus-return behaviour. Run relevant existing component,
type/lint/format checks and all three genuine journeys, followed by READ-ONLY
exact-source review. Do not infer Safari/iOS-wide support from Linux WebKit.
