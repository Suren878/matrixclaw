# Controlplane and API surface (Phase C #6)

Audit: `controlplane-api.md` items 6, 8, 9, 11, 18, the rest of 22, verdict
points 1–3.

## Current shape and what is wrong

- **Three forwarding layers.** `controlplane.New(runtime any, …)` fills 28
  optional interfaces (`dispatcher.go`) by unchecked type assertions and
  guards them with 43 `unsupportedRuntime` checks. Both clients pass the same
  `clientruntime.ControlplaneRuntime` (797 lines; 83 of its 91 methods are
  `client, err := r.client(""); return client.X(…)`), the terminal by
  embedding it in its `Runtime`. A missing method fails silently (the
  `/memory` bug). Owner/guest policy is client-side only
  (`ManagesPermissionMode`, `ManagesRules`); the daemon trusts a
  self-declared `restricted` body field on three requests.
- **Module screens hard-code providers.** `controlplane/modules_voice_*.go`
  (3.0k lines) and `modules_realtime_voice_*.go` (0.9k) plus telephony,
  browser and web search screens (0.7k) know which provider is local, has a
  run mode, needs an engine, which language tables exist, what to do after a
  download (select it, enable the module), after a delete (reselect, disable),
  and start/stop runtimes that C5's `voice.Module.Apply` already reconciles.
  The local catalogs and defaults live in `setup/voice_modules.go`. Zero tests.
- **Five picker models.** `PickerData/PickerItem → PickerViewItem/PickerPage →
  pickerPresentationItem → ResultViewItem/PickerViewData/ResultView`, plus
  `CommandView`; Telegram emoji prefixes, `✅`, paging and "/close to close"
  sit in shared code behind `Surface` switches; Telegram paging re-derives the
  command from `PickerKind` (`picker_commands.go`).
- **Hand-rolled API routing.** 16 `TrimPrefix` sites, `childRoutes`, method
  switches, two JSON decoders, eight setters and 23 "not configured" guards
  production never hits. Skills: anonymous bodies, `map{"ok": true}`, one
  `POST` meaning install or draft, `usage`/`sessions` shadowing skill ids.

## Target shape

### 1. One daemon port

```go
// controlplane
type Dispatcher struct { daemon *daemonclient.Client; workingDir string; now func() time.Time }
func New(daemon *daemonclient.Client, workingDir string) *Dispatcher
func (d *Dispatcher) Handle(ctx context.Context, text string) (Result, error)
```

The client carries client name, external key and the caller's role:

```go
type Role string // daemonclient
const (RoleOwner Role = "owner"; RoleMember Role = "member"; RoleGuest Role = "guest")
// Client.Role is sent as X-Matrixclaw-Role on every request; empty = owner.
```

Telegram builds one client per target (`owner` in the owner chat, `guest` for
inline/guest targets, else `member`) and passes it; the terminal passes its
own client (owner). `clientruntime` is deleted; the terminal `Runtime` holds
`*daemonclient.Client` and calls it directly. The few adaptations the adapter
did (404 binding → none, create+use session, default working dir, automation
client/external key) become dispatcher helpers.

**Daemon-side authorization.** The API reads the role into the request
context; a refused request answers 403 (`core.ErrOwnerOnly`).

- Owner only: changing a session's permission mode, adding/deleting a global
  rule, and (new) every settings write: module settings, setup providers,
  MCP, external agents, the skills library, and admin reload/restart/stop.
  Reads stay open, so non-owner chats keep the read-only status views.
- Guests may keep no rule (also through an approval's "always").
- `member`/`guest` stay out of sessions that run unattended
  (`core.RunsUnattended`); the `restricted` JSON fields go (`json:"-"`,
  filled from the header).

The header is an assertion by a trusted local client: every caller holds the
API token, so the daemon trusts it as it trusts the token. It is never derived
from end-user input: Telegram sets it from the verified sender id of the
update (`ownerChat`), never from message text. Requests without the header
(iOS, CLI, gateway) act as the owner, as now. The dispatcher only hides what
the daemon would refuse (it reads `client.Role`): non-owners see module and
provider screens read-only (status rows, no edit commands), and no server
restart/stop.

### 2. Module settings from the daemon

`internal/modules` gains a settings contract; a module that implements
`Configurable` gets a generic screen:

```go
type Configurable interface {
    Settings(ctx context.Context) []Item                  // may probe (user asked)
    Change(ctx context.Context, path []string, value string) (Change, error)
}
type Change struct {
    Config  func(*setup.Config) error // setup.json edit; nil when none
    Open    []string                  // page to show next; nil = the item's page
    Message string
}
type Item struct {
    Key, Label     string   // Key: [a-z0-9_.-]+, one path segment
    Kind           ItemKind // toggle | choice | text | secret | action | page | info
    Value, Display string   // secret: Value empty, Display a masked preview
    Hint, Confirm  string   // Hint: prompt text or why disabled
    Danger, Disabled bool
    Options []Option        // choice: {Value, Label, Info, Group, Confirm, Disabled}
    Items   []Item          // page
    Facts   []Fact          // info rows
}
```

`Status` gains `settings bool`. HTTP: `GET /v1/modules/{id}/settings` →
`{status, items}`; `POST /v1/modules/{id}/settings` `{path, value}` →
`{settings, open, message}`. The API runs `Change`; when `Config` is set it
calls `setup.Service.Update(Config)` then the supervisor `Reload`, so runtime
start/stop follows from `Apply` as C5 built it. Long actions (engine install,
model download) run inside `Change`; daemonclient uses its 30-minute client
for this call, as it does for voice actions now.

Daemon-side screens (provider knowledge moves here):

| Module | Items |
|---|---|
| tts, stt (`modules/voice/settings.go`) | provider choice (off + local providers; confirm when the engine is missing); per-provider page: installed voices/models (page each: use, delete), add (choice grouped by language), language, style/threads, run mode, engine install/delete, status info |
| realtime_voice | provider choice (unconfigured → open its page); per provider: API key (secret), model, voice, language, advanced (key env, endpoint), status |
| telephony | enabled, gateway URL, token (secret), default profile, phone prompt, status (gateway probe) |
| web_search | provider choice (missing key → open the key), Tavily/Serper keys, SearXNG URL |
| browser | provider choice, engine install/delete, run mode, status |

The flows the client ran become `Change` logic: choosing a provider installs
its engine first and opens "add voice/model" when none is installed;
downloading selects the model and enables the module with its provider;
deleting the active model selects another installed one or turns the module
off; deleting the engine of the active provider turns it off. Local voice
catalogs, defaults and language tables move from `setup/voice_modules.go`
and `controlplane/modules_voice_language.go` to `modules/localruntime` (which
already holds the download catalogs); `setup` keeps only structural trimming
of `VoiceModuleConfig`, and the voice module drops defaults on edit
(as C5 did for realtime). `setup.VoiceModuleDescriptor` and friends become
`localruntime` types.

Controlplane renders any `Configurable` module with one file (~350 lines):

```text
/modules <id>                      root page
/modules <id> open <path> [group]  page, info, or choice picker (group step)
/modules <id> edit <path>          prompt (secret → sensitive)
/modules <id> confirm <path> [v]   confirm, then set
/modules <id> set <path> [value]   POST change, show Open (or the item's page)
```

Paths are `/`-joined keys. `/modules` lists `GET /v1/modules`: modules with
settings and the client's own screens (agents, skills, MCP, storage stay
bespoke; they are collections and editors, not settings).

### 3. One picker model

`Picker{Kind, Title, Meta, Command, Back, Close, Select, Items []PickerItem}`
is what clients render. `Picker.Command` reproduces the picker (Telegram
paging uses it; `picker_commands.go` goes). Deleted: `PickerViewItem`,
`PickerPage`, `PickerFooterAction`, `pickerPresentationItem`,
`PickerViewData`, `ResultView*`, `CommandView`, `CommandMenuResultView`,
`Surface`, `PresentResult`. The command menu is `controlplane.Menu(state)
[]PickerItem`. Telegram owns paging, emoji prefixes, `✅`, bot-command
filtering and "/close to close"; the terminal owns legends and hidden close
footers.

### 4. API

```go
type Deps struct {
    Core *core.Core; Automation *automation.Service; Storage storageStore
    Realtime *realtime.Manager; Modules Modules; Skills *skills.Service
    Setup *setup.Service; Reload func(context.Context) error
    Restart func(context.Context, core.AdminRestartRequest) error
    Stop func(context.Context) error; APIToken string
}
func New(deps Deps) *Server
```

One route table of Go 1.22 patterns (`"GET /v1/sessions/{id}"`), each with an
optional body limit; one `decodeJSON(w, r, &v)` (empty body = zero value).
The supervisor gets the server's reload hooks through `Deps` closures.

**Contract.** Every path the clients use keeps its method and shape (route
test `api/routes_test.go` lists them: daemonclient, telephony gateway
`GET /v1/modules/voice/realtime_voice`, `POST /v1/realtime-voice/sessions`,
`…/{id}/stream`, `POST /v1/messages`, `POST /v1/modules/storage/temp`, and
the Swift package's health, bindings, snapshot, sessions, messages,
approvals, runs, session-providers, setup/providers, storage files/temp,
events). Changes, all Go-client-only (Swift is unaffected):

- Skills: `GET /v1/modules/skills?…&order=usage` (replaces `…/usage`),
  `POST /v1/modules/skills` = install `{path}`, `POST /v1/modules/skills/drafts`,
  `PUT …/{id}/body`, `POST …/{id}/{action}` and `DELETE …/{id}` answer 204;
  session skills move to `GET /v1/sessions/{id}/skills`,
  `POST|DELETE /v1/sessions/{id}/skills/{skill}`. Named types
  `SkillsResponse`, `InstallSkillRequest`, `SkillDraftRequest`,
  `SkillBodyRequest` in `skills`.
- Removed (controlplane-only, replaced by settings): `GET|PATCH
  /v1/modules/voice[/{id}]`, `…/providers/{p}/action`, `PATCH
  /v1/modules/voice/realtime_voice` (GET stays for the gateway),
  `/v1/modules/telephony`, `/v1/modules/web-search`, `/v1/modules/browser`,
  `…/browser/providers/{p}/action`. `POST /v1/modules/voice/tts|stt` stay.
- `restricted` body fields → `X-Matrixclaw-Role` header.

No SQLite change. setup.json: no shape change; voice entries saved with
defaults keep loading and lose them on the next edit.

## Deleted and size

`internal/clientruntime`, the 28 interfaces and 43 guards,
`controlplane/modules_{voice,realtime_voice}_*.go`,
`modules_{telephony,browser,web_search}.go`, `picker_{view,presentation,
commands}.go`, most of `view.go`/`menu_view.go`, the voice/telephony/
browser/web handlers and daemonclient methods, `setup/{telephony,web_search}`
descriptor code and voice catalogs, `api` path parsing and guards.
Rough size: −7.5k / +3k lines (tests +1.2k).

## Commits (each green)

1. `refactor(api)`: route table on mux patterns, `decodeJSON`, `api.New(Deps)`;
   route test pinning every client path.
2. `refactor(skills)`: named types and endpoints; daemonclient and CLI follow.
3. `feat(api)`: caller role header and daemon-side owner/guest checks.
4. `refactor(controlplane)`: dispatcher on `*daemonclient.Client`;
   `clientruntime` gone; tests on an `httptest` fake daemon.
5. `refactor(controlplane)`: one picker model; wording into the clients.
6. `feat(modules)`: settings contract, endpoints, generic renderer;
   telephony, web search, browser screens.
7. `refactor(voice)`: TTS/STT settings and catalogs in the daemon.
8. `refactor(realtime)`: realtime settings; old module endpoints gone.
9. `docs`: ARCHITECTURE, as built.

## Risks and tests

- Paths drift: the route test lists every client path (method + sample
  path, escaped ids with `%2F`) used by daemonclient (terminal, Telegram,
  CLI), the telephony gateway and the Swift package, and asserts the real
  mux routes each one (no 404/405). Removed endpoints are grepped across Go
  and Swift for remaining callers.
- C2 drops `Result.Approval`, `call.Approved` and the approval `action`
  field; approval rendering in the clients is touched only where the picker
  model forces it.
- Authorization: API tests per role (mode change, global/session rules,
  approval always, unattended session) — owner allowed, member/guest refused.
- Voice flows lose client logic: `modules/voice` tests drive `Settings`/
  `Change` with a fake local runtime (choose provider without engine, download
  selects and enables, delete active reselects or disables, run mode) — no
  real downloads. Same for realtime, telephony, web, browser.
- Generic renderer: controlplane tests on a fake daemon serving settings:
  root page, choice with groups, secret prompt is sensitive, confirm then set,
  `Open` after a change, errors as text. Bespoke screens get their first
  tests: skills (list, remove, edit), MCP (toggle, add, delete), storage
  (list, delete, temp cleanup).
- Behaviour changes (deliberate): settings writes and server restart/stop
  are owner-only (non-owner Telegram chats see status only); `/modules` rows show the daemon's status
  line; module pickers lose their TTS/STT/LIVE/TEL label prefixes in
  Telegram; module wording comes from the daemon. Commands, menus, forms and
  paging stay otherwise the same.
- C2 edits `internal/tools` and the core tool pipeline in parallel; this
  branch touches neither.

## As built

Commits follow the plan; non-owner gating of the client's own module screens
is a commit of its own. Differences from the design:

- Settings live at `GET|POST /v1/settings/{module}`, not
  `/v1/modules/{id}/settings`: that pattern overlaps `GET
  /v1/modules/skills/{id}` in the mux with neither more specific.
  `modules.Change` gained `Reload` for changes that alter what modules offer
  without editing setup.json (installing the browser engine adds its MCP
  server). Text and secret values: an empty prompt keeps the value, `-`
  clears it (the client sends ""); the daemon stores what it is sent.
- One JSON decoder treats an empty body as the zero value; the
  permission-mode endpoint now refuses an empty mode instead of resetting it.
  The mux answers unknown paths and methods with its plain-text 404/405.
- Voice: the local voice descriptor types and fallback catalogs moved to
  `localruntime` (`VoiceModule`, `VoiceProvider`, `VoiceModel`); setup keeps
  the persisted shape, its defaults (so setup.json keeps dropping them on
  save) and the Supertonic/Whisper language tables, now the only copies.
  Piper models carry `Country` instead of it being parsed from
  descriptions. `localruntime.Runtime.Offline` keeps the bundled catalogs
  (tests use it). Choosing a provider whose model is missing selects another
  installed one when there is one; the Supertonic storage walk of cache
  directories and the per-provider "Installed" confirmations are gone.
- Realtime: `Manager.Edit` and `setup.VoiceModuleUpdate` went with the
  PATCH endpoint; an API key can now be cleared (`-`).
- Telegram's own daemon client (deliveries, lookups) acts as a `member`;
  only `daemonFor(target)` acts as the owner, so the restart interceptor
  now runs with the chat's role.
- Kept as they were: MCP paths (`/v1/modules/mcp/{id}/server`), the storage
  and voice TTS/STT execution paths, `GET /v1/runs/{id}/steps`.

Size (against main at 99d625b): Go outside tests +3.7k / −9.9k, tests
+1.6k / −0.7k; 208 files.
