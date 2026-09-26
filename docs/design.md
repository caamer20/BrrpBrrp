# BrrpBrrp interface

A familiar community chat layout with an identity of its own: a compact channel sidebar, uninterrupted message timeline and optional member panel. A small green BrrpBrrp mark, cool neutral surfaces and restrained accent colors distinguish it from Discord. Avoid promotional copy inside the app.

## References

- [Emil Kowalski's design engineering skill](https://github.com/emilkowalski/skills/tree/main/skills/emil-design-eng): immediate keyboard interactions, short purposeful panel transitions, press feedback and reduced-motion support. Read and applied as project guidance; no runtime animation library is needed.
- [DesignMD's Discord reference](https://www.designmd.co/d/discord): layered navigation surfaces, compact grouping, rounded controls and touch targets. The catalog describes the marketing site and includes inferred patterns; it is inspiration, not a specification of Discord's app.

## Product rules

- Light, dark and system themes have persisted local preferences.
- Use system fonts, readable body text and tabular timestamps; avoid shipping a font download.
- Keep channel switches, typing and message arrival immediate.
- Show voice details before the initial join, then switch rooms directly from the sidebar. Keep the panel available from the voice controls and show connection errors inline.
- Let desktop users collapse the members panel from its header and restore it from an arrow in the conversation header. Keep search, pins and that arrow in normal layout flow. Remember the choice on the device and restore keyboard focus when the panel closes.
- Use native buttons, inputs and dialogs with visible keyboard focus. Icon buttons have accessible names.
- Show empty, loading, disconnected and error states honestly. Never populate real channels with pretend users or messages.
- Keep destructive confirmations specific: name the channel, message or account and state the effect.
- On phones the channel list becomes a drawer; the composer remains reachable above the keyboard.

## Code conventions

Prefer ordinary names, small modules organized by responsibility, explicit error handling and focused tests of behavior. Comments explain constraints or tradeoffs. Avoid generated-code banners, meta-commentary, unnecessary wrappers and speculative abstractions.
