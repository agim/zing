# Decisions

What shaped zing and why, one entry each, newest last: a pack added, Rust
chosen for a module, a dependency taken, a schema tradeoff, an
integration. Read this before working in those areas. Record yours in the
same commit as the change: `lidza decision add "Title" --why "..."`
(MCP: `lidza_decision_add`). A recipe in docs/lidza-guide.md says how
this app does something; a decision here says why it is done that way.

## 2026-11-05: Pack auth added

Why: Zing requires durable identity, publishing, email, realtime and media services

Touches: lidza.json, packs.go

## 2026-11-05: Pack jobs added

Why: Zing requires durable identity, publishing, email, realtime and media services

Touches: lidza.json, packs.go

## 2026-11-05: Pack mail added

Why: Zing requires durable identity, publishing, email, realtime and media services

Touches: lidza.json, packs.go

## 2026-11-05: Pack cache added

Why: Zing requires durable identity, publishing, email, realtime and media services

Touches: lidza.json, packs.go

## 2026-11-05: Pack realtime added

Why: Zing requires durable identity, publishing, email, realtime and media services

Touches: lidza.json, packs.go

## 2026-11-05: Pack storage added

Why: Zing requires durable identity, publishing, email, realtime and media services

Touches: lidza.json, packs.go

## 2026-11-05: Who owns the data: Each user exclusively owns their connections, credentials, drafts, me…

Why: Answered in the brief interview (docs/brief.md): Each user exclusively owns their connections, credentials, drafts, media, schedules and deliveries. Realtime topics require the same owner. Every query is scoped by it; the most expensive answer to change later.

Touches: docs/brief.md

## 2026-11-05: Sign-in: Independent email/password through Lidza auth.Mount, email verificati…

Why: Answered in the brief interview (docs/brief.md): Independent email/password through Lidza auth.Mount, email verification, reset, sessions and account deletion; social connection is separate. Configures auth.Mount and the sign-in page.

Touches: docs/brief.md

## 2026-11-05: Palette: Use supplied Zing SVGs and electric blue #0085F3, black and white; fo…

Why: Answered in the brief interview (docs/brief.md): Use supplied Zing SVGs and electric blue #0085F3, black and white; follow device theme with manual override. Written into the design tokens (src/index.css) and the admin theme; agents never fall back to a stock blue.

Touches: docs/brief.md

## 2026-11-05: Pack db added

Why: PostgreSQL owns user data, durable delivery states, transactional job enqueue and per-account isolation.
