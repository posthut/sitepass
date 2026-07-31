# Sitepass — Design Contract

Binding for all interface work. A change to any value here is a change to
this document first.

---

## 1. Direction

Calm, comfortable minimalism.

The product is a link that expires. The interface has two jobs: hand over
a token, and show how much time is left. Everything else is subordinate.

Two consequences that shape every decision below:

**One screen, no navigation.** There is no dashboard, no settings, no
account. The control site is a single view that changes state. Adding a
nav bar to a one-screen product is decoration.

**The interface is a handover counter.** The user is not staying. They
came to collect something and take it to their agent. The design should
feel like a well-organised counter, not a workspace.

Minimal directions are judged on precision. Spacing, alignment and type
must be exact; there is nothing else to look at.

---

## 2. Colour

Neutrals are cooled slightly toward the accent rather than warmed. This is
deliberate: the warm-cream-and-terracotta palette is the house style of
every generated landing page, and this product should not look like one.

### Light

    canvas        #F9FAF9    page background
    surface       #FFFFFF    cards, raised areas
    sunken        #F1F3F1    inset areas, code blocks, disabled fills
    line          #E2E5E2    all borders and rules
    ink           #16191A    primary text
    ink-muted     #666D6B    secondary text, labels
    ink-faint     #9AA09E    placeholders, disabled text
    accent        #1F6B4F    primary action, live state
    accent-hover  #185840
    accent-wash   #E7F0EB    accent background tint
    warn          #8A6A24    expiring soon, non-blocking warnings
    danger        #8C3A32    errors, destructive actions

### Dark

    canvas        #121413
    surface       #1A1D1B
    sunken        #222624
    line          #2E3331
    ink           #EAEDEB
    ink-muted     #9AA29E
    ink-faint     #6C736F
    accent        #62A585
    accent-hover  #7BB89A
    accent-wash   #1C2A23
    warn          #C9A45C
    danger        #C8756A

Dark mode follows `prefers-color-scheme` with a manual override persisted
in `localStorage`. Both are first-class; neither is an afterthought.

### Rules

One accent. Green carries a single meaning throughout: **this preview is
alive**. It appears on the primary action, the live indicator, and the
countdown while healthy. It is never used for emphasis or decoration.

Colour is never the only carrier of meaning. Every state is colour plus
icon plus text.

Contrast floors: 4.5:1 for text, 3:1 for interactive boundaries and icons.

---

## 3. Typography

Two families, split by who is speaking.

    Human text    Onest             400 / 500 / 600
    Machine text  JetBrains Mono    400 / 500

This split is the product's structure, not a stylistic pairing. Tokens,
preview URLs, the countdown, file counts and byte sizes are objects the
machine produced and the machine will consume. They are set in mono. Every
sentence addressed to a person is set in Onest.

Applied consistently, the user learns without being told which text is
meant to be copied and which is meant to be read.

Onest is chosen over the usual neutral grotesques because it was drawn
with Cyrillic as a first-class script rather than an extension, and it
covers the Kazakh letters ә ғ қ ң ө ұ ү һ і across all weights used. This
is a hard requirement, not a preference — a missing glyph in Kazakh is a
broken interface.

Both families are self-hosted as WOFF2, subset to Latin plus Cyrillic
extended. No external font CDN: it adds a third-party dependency and a
request to a domain the operator does not control.

### Scale

    display   40 / 44    600    one per page, the headline
    title     24 / 30    600    state headings
    body-lg   18 / 28    400    lead paragraph
    body      16 / 24    400    default
    small     14 / 20    400    helper text, warnings
    label     12 / 16    500    eyebrows, field labels, uppercase, 0.06em
    mono-lg   18 / 28    500    the token, the preview URL
    mono      14 / 20    400    counts, sizes, error codes

Only these. A size not in this table does not get used.

Measure is capped at 62 characters for body text. Headline copy is capped
at three lines at the narrowest breakpoint.

Numerals in the countdown use `font-variant-numeric: tabular-nums` so the
digits do not shift as they change. A countdown that jitters draws the eye
every second, which is the opposite of calm.

---

## 4. Space

Base unit 4. Scale: 4, 8, 12, 16, 24, 32, 48, 64, 96.

    Card padding            24 (mobile 20)
    Between related items   12
    Between groups          24
    Between sections        48
    Page top margin         96 (mobile 48)

Content column 640. The page is one column, centred, at every width.
There is no wide layout, because there is nothing to put beside the card.

White space is the primary compositional tool. When a screen feels
unfinished, the answer is to remove an element, not to add one.

---

## 5. Shape and depth

    radius-sm    6     inputs, small controls
    radius-md    10    buttons, cards
    radius-full  999   status pills

Separation comes from a 1px `line` border and a `sunken` fill. Shadows are
used in exactly one place: elevated overlays, at
`0 2px 8px rgba(0,0,0,0.06)`.

No gradients. No glass. No decorative borders.

---

## 6. Signature element: the expiry rule

The one memorable object in the interface.

A 2px horizontal rule spans the full width of the token card, directly
beneath its top edge. It represents the remaining lifetime and shortens
from right to left in real time. Beside it, the remaining time is set in
tabular mono.

    ┌─────────────────────────────────────────┐
    │ ████████████████████████░░░░░░░░░░░░░░░ │  ← the rule
    │                                         │
    │  UPLOAD TOKEN                    24:13  │
    │  pv_k7m3x9q2h8n4c6v1b5z0                │
    │                                         │
    │  [ Copy instruction for agent ]         │
    │  Copy token                             │
    │                                         │
    │  PREVIEW                                │
    │  landing-k7m3x9q2h8.preview.example  ↗  │
    └─────────────────────────────────────────┘

Behaviour:

- Above 5 minutes remaining: `accent`
- At or below 5 minutes: transitions to `warn` over 600 ms, once
- At zero: the rule is empty, the card falls to `sunken`, the URL is
  struck through, and a single action remains — create a new token

It animates by `transform: scaleX()` on a 1-second interval, not by
redrawing width. Under `prefers-reduced-motion` the rule updates in
discrete steps every 10 seconds and the colour transition is instant.

It is silent. No pulsing, no ticking, no sound, no notification. It is
readable at a glance from across the desk and ignorable when it is not
needed. That restraint is the point: the user is watching their site, not
this page.

---

## 7. Components

### Buttons

One primary per screen. Everything else is quiet.

    primary     accent fill, white text, radius-md, 44px tall
    secondary   1px line border, ink text, transparent fill
    quiet       no border, ink-muted text, underline on hover

All five states are specified and implemented for every button:
`default`, `hover`, `active`, `disabled`, `loading`. A loading button keeps
its width and replaces its label with a 16px spinner; it does not collapse
or shift the layout.

### Copy controls

The two copy actions are ranked, because they are not equal. **Copy
instruction for agent** is primary — it is what the product is for. **Copy
token** is quiet, for people who know what they are doing.

Confirmation is an in-place icon swap held for 1.5 s. No toast. A toast for
an action the user just performed, whose result is visible, is noise.

### Status pill

    waiting     sunken fill,     ink-muted    "Waiting for upload"
    live        accent-wash,     accent       "Live"
    expired     sunken,          ink-faint    "Expired"

Icon plus text, always both.

### Inputs

One field exists: project name. Optional, 48 characters, with a live
character count appearing only past 40. Its helper text states what the
name affects — the address and nothing else.

### Warning list

Warnings from an upload appear beneath the URL as plain rows: `warn` icon,
`mono` code, `small` text. Collapsed past three, with a count. They are
information, not alarms, and they never occupy more visual weight than the
URL above them.

---

## 8. Layout and breakpoints

    sm   640
    md   768
    lg   1024

Single column at all widths. The breakpoints adjust padding and type
scale, not structure.

The mobile and desktop interfaces are the same interface. Every action
available on one is available on the other, in the same order, with the
same labels. There is no "mobile version".

Mobile specifics:

- Touch targets 44px minimum
- Token and URL wrap with `overflow-wrap: anywhere`, never truncate with
  an ellipsis — a truncated token cannot be verified by eye
- The copy button sits within thumb reach at the card's lower edge
- Text inputs are at least 16px to prevent iOS zoom on focus

---

## 9. Motion

    micro       120 ms    cubic-bezier(0.2, 0, 0, 1)    hover, focus, icon swap
    transition  240 ms    cubic-bezier(0.2, 0, 0, 1)    state change
    expiry rule 1000 ms   linear                        the countdown only

Only `opacity` and `transform` are animated.

There is no page-load animation, no scroll reveal, no ambient movement.
The one moving element on the page is the expiry rule, and it moves
because it is measuring something real.

Under `prefers-reduced-motion: reduce`, all durations become 0 except the
expiry rule, which steps every 10 seconds.

---

## 10. Accessibility

Not a checklist item; part of the definition of done.

- Every action reachable and operable by keyboard
- Focus ring always visible: 2px `accent`, 2px offset, never removed
- Landmarks and heading order correct; one `h1`
- The countdown is `aria-live="off"` with an accessible label updated at
  10-minute, 5-minute and 1-minute boundaries only. A per-second live
  region would make the page unusable with a screen reader
- Copy confirmation announced via a polite live region
- Errors associated with their control via `aria-describedby`
- `lang` attribute set correctly per language, including `kk`
- Full operation at 200% zoom without horizontal scrolling

---

## 11. Copy

Written from the user's side of the screen. Plain verbs, sentence case, no
exclamation marks, no filler.

Name things by what the person controls. "Upload token", not "API
credential". "Preview", not "deployment".

A control names exactly what happens: **Create upload token**, not
**Submit**. The same word carries through the flow — the button says
"Create upload token" and the resulting label says "Upload token".

Errors state what happened and what the limits are. They do not apologise,
do not blame, and do not give advice about the user's build tooling:

    Good:  Archive is 142 MB. The limit is 100 MB.
    Bad:   Oops! Your archive is too large — try removing node_modules.

Empty and expired states are invitations, not apologies:

    Waiting for your agent to upload.
    This preview has expired. Create a new token to publish again.

Every string lives in `locales/*.json`. Translations are written, not
generated — an interface with eleven strings has no excuse for machine
translation. Kazakh is reviewed by a speaker before release.

---

## 12. Token export

Every value in this document is emitted as a CSS custom property from a
single source file, and nothing consumes a raw hex value or a raw pixel
number.

    --canvas, --surface, --sunken, --line
    --ink, --ink-muted, --ink-faint
    --accent, --accent-hover, --accent-wash, --warn, --danger
    --font-human, --font-machine
    --text-display … --text-mono
    --space-1 … --space-9
    --radius-sm, --radius-md, --radius-full
    --duration-micro, --duration-transition, --ease

A literal colour or spacing value in a component is a review rejection.

---

## 13. Self-hosting and brand

An operator may change `SITEPASS_BRAND_NAME` and replace the logo mark.
Nothing else in this document is configurable, and no theming system is
provided.

The reason is maintenance cost: a theming layer must be kept working
across every change to every component, in exchange for a benefit almost
no self-hoster asks for. Anyone who wants a different look has the source.
