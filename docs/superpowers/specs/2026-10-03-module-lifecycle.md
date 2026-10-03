# Module lifecycle (Phase C #5)

Audit: `telegram-modules-setup.md` items 5, 7, 11, 15, 21 and verdict points
2–3; `controlplane-api.md` items 5, 13, 14, 22 and verdict point 3.

## Current shape and what is wrong

- **No module definition.** `modules.Module` (`internal/modules/registry.go`)
  is `RegisterTools` + `Context`, implemented by storage, MCP and skills only.
  Voice, telephony, delivery, geo and web are loose executors in `run.go`'s
  `extraTools`; browser lives in setup, localruntime and `mcpConfigWithBrowser`.
  Each module reads `setup.json` on its own: the TTS tool, voice HTTP handlers,
  telephony tools and web search load and parse the file per call;
  `setupAwareToolExecutor` (`daemoncmd/tool_visibility.go`) wraps the whole
  tool executor to hide `telephony_call`, re-reading setup.json on every
  `List`/`Spec` (every model step) and leaving `telephony_end_call` visible.
- **Config changes do not reach modules.** `supervisor.Reload` re-applies
  session LLMs, external agents and Telegram (those now diff), but not the
  modules. MCP is built once at start and the API answers
  `restart_required` for MCP and browser edits; an MCP server that fails to
  connect disables every MCP server. Nothing is closed on shutdown except MCP
  and skills.
- **Local runtimes.** `localruntime.Runtime` is a stateless facade rebuilt per
  call (15 `localruntime.New("")` sites). whisper, supertonic and piper each
  repeat lock/check/stop/start/wait-ready around package-global maps
  (`*_process.go`, ~580 lines); the global lock is held during the up-to-90 s
  readiness probe; children are not stopped on daemon exit and have no
  `Pdeathsig`; RSS is found by scanning `/proc` for process names, so orphans
  and unrelated `main` processes are counted. `voiceProviderDriver` has 15
  methods.
- **Realtime providers are copy-paste.** gemini/grok/openai (3.2k lines) each
  carry the same connection (dial, setup wait, read loop, emit, write, close),
  key-check cache, `maskSecret`, `truncateReason`, `secretCacheKey`, config
  normalisation, and (gemini/grok) schema sanitising and instruction joining.
  Defaults and language tables exist again in `setup/voice_modules.go`
  (defaults + `normalizeRealtimeVoiceLanguageCode`/`normalizeGrokVoice…`) and
  in `controlplane/modules_realtime_voice_options.go`.
  `daemoncmd/realtime_voice.go` resolves each provider's config from setup
  overlaid by ~17 `MATRIXCLAW_*` env overrides, re-loading setup.json on every
  `Descriptor`/`Connect`.
- **Telephony.** Phone prompt and assistant name travel in every
  `POST /v1/calls` with gateway env fallbacks
  (`MATRIXCLAW_TELEPHONY_PHONE_PROMPT`, `…_ASSISTANT_NAME`); inbound calls get
  neither. The gateway decides it is healthy by comparing the realtime
  module's human status text with `"Ready"`, and the daemon reads the
  gateway's `"status": "ready"` string. Inbound calls accept every caller when
  the allowlist is empty. The call and end-call tools and the API probe each
  build the gateway URL, auth header and HTTP call.
- **Restart notice has its own pipeline.** The daemon saves a
  `daemon_restart` delivery, and on start pushes Telegram ones itself through
  a second Bot API client (`telegram.RestartDeliverySender`,
  `RestartDeliveryCodec`), and marks the rest `ready` for the terminal.

## Target shape

### `internal/modules`: one lifecycle

```go
// Module is a daemon feature configured from setup.json.
type Module interface {
    ID() string
    // Apply makes the module follow cfg. Called at start and after every
    // setup change, serialized; cheap when the module's part is unchanged.
    Apply(ctx context.Context, cfg setup.Config) error
    Tools() []tools.Executor   // offered now; none while off or unconfigured
    Context() string           // system prompt paragraph, "" for none
    Status(ctx context.Context) Status // no network I/O beyond cached results
    Close() error
}

type Status struct {
    ID      string   `json:"id"`
    Title   string   `json:"title"`
    Enabled bool     `json:"enabled"`
    Ready   bool     `json:"ready"`
    State   string   `json:"state"`            // "Local · running", "Gateway URL required"
    Detail  string   `json:"detail,omitempty"` // why not ready / next step
    Facts   []Fact   `json:"facts,omitempty"`  // ordered rows for a generic UI and the status note
    Tools   []string `json:"tools,omitempty"`  // filled by Set
}
type Fact struct{ Key, Label, Value string }

// Set owns the modules and is the daemon's core.ToolExecutor.
func NewSet(base []tools.Executor, mods ...Module) *Set
func (s *Set) Apply(ctx context.Context, cfg setup.Config) error // every module, then rebuild tools
func (s *Set) Context() []string
func (s *Set) Statuses(ctx context.Context) []Status
func (s *Set) Close() error
// List/Spec/Execute/Subject/ConcurrencyKey read an immutable *tools.Registry
// (base + modules' Tools()) swapped atomically after each Apply.
func Static(id, title, context string, executors ...tools.Executor) Module
```

Modules (constructed once in `daemoncmd/modules.go`, closed on shutdown):

| ID | Package | Tools | Apply |
|---|---|---|---|
| storage, geo, delivery | `Static` | storage_*, osm_*, send_file | — |
| web | `modules/web` (new, small) | web_fetch, web_search | stores the search provider config |
| tts, stt | `modules/voice` (`voice.New(rt, kind)`) | text_to_speech while TTS is on | stores config; starts the selected `always_running` runtime in the background, stops runtimes no longer selected |
| realtime_voice | `modules/voice/realtime` (Manager) | — | resolves provider configs once (see below) |
| telephony | `modules/telephony` | telephony_call, telephony_end_call while enabled with a gateway URL | stores config, one gateway client |
| browser | `modules/browser` (new, from api/setup glue) | — (its tools come through MCP) | stores config; `MCPServer(cfg)` for the MCP module |
| mcp | `modules/mcp` | remote tools of connected servers | diffs wanted servers (setup + browser) per server id: closes removed/changed sessions, connects new ones; a failing server shows in Status, others keep working |
| skills | `modules/skills` | skill_* while enabled | updates the service's enabled/auto-invoke/trust policy |

Tool visibility therefore follows module state with no wrapper and no file
reads per step; `daemoncmd/tool_visibility.go` goes. Automation, shell, todo,
await, memory and agent tools are the Set's `base`.

### Supervisor: one Apply

`supervisor.Reload(ctx)` (the only entry, used by `POST /v1/admin/reload`, the
setup wizard and every API handler after a setup write, MCP/browser/web
included) loads setup.json and, under `reloadMu`, applies: session LLMs →
external agents (diff, as now) → `modules.Set.Apply` → assistant profile with
module contexts → Telegram (diff, as now). Parts whose config did not change
do nothing, so a voice edit no longer touches anything else.
`bootstrapConfig` keeps process-level settings and the loaded `setup.Config`
(the `ExternalAgents ModulesConfig` field goes). Start-up order: bind the
listener, `Reload`, serve, release held deliveries, then recovery (C1's
recovery block stays as it is; module wiring moves out of `run.go` into
`daemoncmd/modules.go` to keep the merge small).
`restart_required` (`MCPConfigResponse`, `BrowserModuleDescriptor`,
`api.SetMCPChanged`, `mcpConfigChanged`) and the status note's
`unavailable_restart_required` go; browser install/delete actions call
`Reload` so the browser MCP server appears or disappears at once.

`GET /v1/modules` (new) returns `{"modules": [Status…]}` for a generic UI
(C6). The runtime status note (`daemoncmd/runtime_status_prompt.go`) becomes
one line per `Status` (`id: enabled; ready; state; facts; tools=N`) plus the
external agents line.

### `internal/procsup`: one process supervisor

```go
type Spec struct {
    Key   string            // one process per key (provider id)
    Path  string; Args, Env []string
    Stdin bool              // keep a stdin pipe (piper)
    Log   string            // stderr log file in the runtime dir; "" discards
    Ready func(ctx context.Context) error // nil: started is ready
    ReadyTimeout time.Duration
}
func New(logDir string) *Supervisor
// Start reuses a running process started from an equal spec; otherwise it
// replaces it. The readiness probe runs outside the lock; concurrent Starts of
// one key wait for the same probe. A failed probe stops the process.
func (s *Supervisor) Start(ctx context.Context, spec Spec) (*Process, error)
func (s *Supervisor) Stop(key string) error
func (s *Supervisor) Running(key string) (*Process, bool)
func (s *Supervisor) StopAll()
func Prepare(cmd *exec.Cmd) // Pdeathsig=SIGKILL on Linux; no-op elsewhere
```

`localruntime.New(root string, procs *procsup.Supervisor)` is built once in
`daemoncmd` and shared by the voice and browser modules; the API and voice
service stop constructing their own. Drivers lose `startRuntime`,
`stopRuntime`, `stopForDelete`, `runtimeRunning`, `processNames` and gain
`server(r, provider) (procsup.Spec, error)` (command + probe). RSS comes from
the supervised PID. One-shot commands (whisper-cli, piper per task, installs)
and the Playwright MCP stdio command call `procsup.Prepare`; Playwright stays
owned by its MCP session because the stdio transport must own the pipes.

### Realtime voice: codecs over one session engine

In `internal/modules/voice/realtime`:

```go
type ProviderSpec struct {
    ID, Name, Endpoint, DefaultModel, DefaultVoice string
    KeyEnvs      []string   // e.g. OPENAI_API_KEY
    LLMProviders []string   // setup provider ids whose key it reuses
    Models, Voices []string // Models nil: CheckKey lists them (Gemini)
    Languages    []Language // {Code, Name}; first is "auto"
    NormalizeLanguage func(string) string
    CheckKey func(ctx context.Context, key string) ([]string, error)
    Dial     func(cfg ProviderConfig, model string) (url string, h http.Header, err error)
    NewCodec func(ProviderConnectRequest) Codec
}
type Codec interface {
    Setup() ([]any, error)              // sent after dialing
    SetupDone(msg []byte) (bool, error) // reading stops at true
    Encode(ProviderInput) ([]any, error)
    Decode(msg []byte) []ProviderOutput
}
```

One `wsSession` (dial with timeout, setup handshake, read loop → output
channel, serialized writes, close once) and one provider implementation
(descriptor status, cached key check, connect) serve all three; shared
helpers: instruction joining + language policy, schema sanitising (Gemini,
Grok), `setup.MaskSecret`. `gemini`, `grok`, `openai` keep only their spec
(catalog) and codec (messages, OpenAI's resampler and decoder state).
`ProviderDescriptor` gains `languages` and `default_voice`;
`ModuleDescriptor` gains `ready`.

The realtime module's `Apply` resolves each provider once: stored setting,
else catalog default; key = `api_key`, else `$api_key_env`, else `KeyEnvs`,
else the key of a configured LLM provider in `LLMProviders`. Assistant
identity, the user's custom instructions and (for `client == "telephony"`)
the phone prompt are composed here for every session. The realtime section of
setup.json is edited by `realtime.Edit(*setup.VoiceModuleConfig, update)`
inside `setup.Service.Update` (validates the provider, keeps the stored key,
drops values equal to the catalog's); setup keeps only structural trimming for
it and loses the realtime defaults, provider list and language tables.
`daemoncmd/realtime_voice.go` goes. The controlplane realtime language picker
reads `provider.Languages` instead of its own tables.

### Telephony

- `modules/telephony/gateway.go`: one client (`PlaceCall`, `EndCall`,
  `Health`) used by both tools and the module's `Descriptor(ctx)` (the API's
  `decorateTelephonyModule`/`probeTelephonyGateway` move there).
- Gateway `/v1/health` answers `{"ready": bool, "error": …, …}` instead of
  `status` text; the daemon reads `ready`, and the gateway reads the realtime
  descriptor's `ready`. `TelephonyModuleDescriptor` gains `ready`.
- The daemon owns identity: `POST /v1/calls` loses `assistant_name`,
  `phone_prompt` and `assistant_custom_instructions`; the gateway loses
  `MATRIXCLAW_TELEPHONY_PHONE_PROMPT` / `…_ASSISTANT_NAME` and those prompt
  sections; inbound calls now get the configured phone prompt too. Every
  other gateway variable keeps its meaning (pinned by a config test); inbound
  calls still use `…_INBOUND_GREETING` and `…_INBOUND_PROMPT`, on top of the
  daemon-composed identity and phone prompt.
- **Behaviour change:** inbound calls are rejected unless
  `MATRIXCLAW_TELEPHONY_INBOUND_ALLOWED_CALLERS` lists the caller; the gateway
  logs a warning at start when inbound is on with an empty list.

### Restart notice as a normal `notice`

The restart request stores a `notice` delivery with the new status `held`
(summary "Daemon restarted.", the requester's address incl. Telegram
`message_id`). On start the daemon releases held deliveries to `pending`
(`Core.ReleaseHeldClientDeliveries`). Telegram's `deliverNotice` edits the
message when the address has a `message_id`, else sends; the terminal polls
its notice and treats `pending` as "restarted". Deleted:
`clients/telegram/restart_delivery.go`, the daemon's Bot API sender and
address codec, `markPullClientRestartDeliveriesReady`,
`deliverPendingRestartNotifications`, `ClientDeliveryTypeDaemonRestart`,
`ClientDeliveryStatusReady`.

## Contracts and data

- HTTP: `restart_required` removed (controlplane only); `ready` added to the
  realtime, telephony and gateway health payloads; `languages`/
  `default_voice` added to realtime providers; `GET /v1/modules` added;
  gateway `POST /v1/calls` drops three fields (daemon and gateway ship
  together; an old gateway ignores the missing ones). Swift is unaffected
  (it uses storage only); `daemonclient` gains `Modules`.
- SQLite: idempotent statement in `applyCanonicalSchema`: `daemon_restart`
  rows become `notice`, `pending`→`held`, `ready`→`sent`; tested on an
  old-shape database, applied twice.
- setup.json: no shape change. Realtime entries saved by older versions
  load as before (languages are normalized by the provider when used).
- **Dropped env overrides:** `MATRIXCLAW_REALTIME_VOICE_{ENABLED,PROVIDER,
  MAX_SESSIONS,MODEL,LANGUAGE}` and `MATRIXCLAW_{GEMINI_LIVE,GROK_VOICE,
  OPENAI_REALTIME}_{API_KEY,MODEL,VOICE,LANGUAGE,WS_URL}`; keys still come
  from `api_key_env` and the standard `GEMINI_API_KEY`/`GOOGLE_API_KEY`/
  `XAI_API_KEY`/`OPENAI_API_KEY`. README documents setup.json only. The lead
  should check the production daemon environment for these names before
  merging.

## Deleted and size

`daemoncmd/{realtime_voice,tool_visibility}.go`, `modules/registry.go`,
`localruntime/{managed_process,whisper_process,supertonic_process}.go` and the
process half of `piper_process.go`, name-based RSS scanning, the three
providers' connection/config/key-cache copies, setup's realtime tables,
controlplane's realtime language tables, `api.decorateTelephonyModule`,
`telegram/restart_delivery.go`, `mcp.ClientModule` (per-server sessions
instead). Rough size: −2300 / +1300 lines.

## Commits (each green)

1. `refactor(localruntime)`: `procsup` + one shared `Runtime` built in
   daemoncmd; drivers give server specs; RSS by PID; `Pdeathsig`; StopAll on
   shutdown.
2. `refactor(realtime)`: session engine, provider specs and codecs, config
   resolved without env overrides, `realtime.Edit`, setup and controlplane
   tables gone, `ready` in the descriptor and gateway probe.
3. `refactor(modules)`: `Module`/`Set`/`Static`; voice, realtime, telephony
   (gateway client), web, skills, storage, geo, delivery as modules;
   supervisor applies the Set; tool visibility from modules; status note from
   `Status`; `GET /v1/modules`.
4. `feat(mcp)`: browser module; MCP per-server sessions applied live;
   `restart_required` removed.
5. `refactor(telephony)`: daemon-owned identity and phone prompt, gateway
   `ready`, inbound deny-all.
6. `refactor(daemon)`: restart notice as a held `notice`, store migration,
   Telegram/terminal follow.
7. `docs`: ARCHITECTURE, README, VOICE/BROWSER/MCP, as-built notes.

## Risks and tests

- Live MCP apply under the supervisor lock can block one API call for a
  server's connect timeout; servers connect in parallel and only changed ones
  reconnect. Test with an in-process MCP server: add, change, remove, failing
  server, tools re-registered.
- Tools swapped mid-run: a removed tool answers "unknown tool". Set test:
  telephony tools appear/disappear with config; Apply unchanged → same
  registry.
- Process supervisor: tests with a small helper process (re-exec of the test
  binary): equal spec reuse, replace on change, probe failure stops, concurrent
  Start runs one process, StopAll, probe does not block other keys.
- Codecs: golden encode/decode tests per provider (OpenAI's existing ones
  ported); session engine against an `httptest` websocket server (setup
  handshake, read loop, close). No real provider requests.
- Prompt changes (phone prompt order, custom instructions in all realtime
  sessions, status note format) are model-facing; covered by instruction
  composition tests.
- Overlap: C1 and C7 are merged (this branch is rebased on them); controlplane
  (C6 later) gets only forced edits.

## As built

Commits follow the plan (the gateway env contract and lint gate were added
before implementation). Differences from the design:

- `modules.Set.Apply` applies every module, then rebuilds the tool registry;
  the supervisor applies the set before the assistant profile so module
  paragraphs are current. At start the daemon binds its listener, applies the
  setup, then serves, so no request sees the base tools only.
- The model's status note shows `enabled`, `ready`, facts and the module's
  visible tool count, never `state`, so it does not change while a runtime
  starts or stops. `Status` makes no network calls: realtime reads the last
  cached key check, telephony does not probe the gateway (its descriptor
  endpoint still does).
- Skills: `Apply` shows or hides the skill tools, prompt paragraph and skill
  prompt; the trust policy and auto-invoke still apply at start.
- Voice: TTS and STT are two `voice.Module`s on one runtime. Runtimes are
  started/stopped only when the module's settings change, so a manual stop
  survives unrelated reloads. Local voice catalogs and defaults stay in
  `setup` (C6 can move them with the settings descriptors).
- Realtime: the realtime section of setup.json is only trimmed by `setup`;
  `Manager.Edit` drops values equal to the catalog's on the next edit, so
  defaults written by older versions stay until then. The LLM-provider key is
  matched by provider id (`gemini`, `xai`, `openai`), no longer by base URL.
  All three providers now use the same language policy text, and the user's
  custom instructions reach every realtime session, not only phone calls.
- MCP closes removed or changed sessions before connecting new ones (in
  parallel); the browser's Playwright server stays owned by its MCP session
  (with `Pdeathsig`) rather than `procsup`.
- Restart notice: a `notice` with payload `{"replace":true}` (so other notices
  that carry a message id are never edited) and status `held`; the store
  conversion also marks old `ready` rows `sent`. `MarkClientDeliveryReady`
  and the `ready` status are gone.
- `TelephonyModuleDescriptor.realtime_module_id` was dropped (setup no longer
  knows the realtime module); `ready` was added to it.
- No `daemonclient.Modules` yet: nothing reads `GET /v1/modules` until C6.

Size: about −5.4k / +3.5k lines of Go outside tests (+1.0k net test lines).
