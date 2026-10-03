# Typed setup config (Phase C #4)

Audit: `telegram-modules-setup.md` items 6, 9 (rest), 17 (rest), verdict point
1; `controlplane-api.md` item 20.

## Current shape and what is wrong

`internal/setup` (5.8k lines) holds the persisted `Config`, but most provider
code works on other shapes:

- `Draft` / `ProviderDraft` (`types.go`): the TUI wizard's model, with
  "yes"/"no" bools (`BoolString`/`ParseBool`) and numbers as strings. It is
  persisted as `setup.json.draft` after every wizard form and is preferred by
  `Service.Draft()`, so `ProviderSetupItems()` (the HTTP list) can show the
  values of an abandoned wizard.
- API provider edits (`ConfigureProviderContext`, `DeleteProviderContext`,
  `session_llm.go` model switch) convert the saved config to a `Draft`
  (`draftFromConfig`), patch it, and rebuild the whole `Config`
  (`buildConfig` + `buildProviderConfig`, `service_config.go`), which also
  calls Telegram `getMe`: a session model switch fails when Telegram is down.
- Six provider shapes with converters: `ProviderConfig`, `ProviderDraft`,
  `ProviderOption` (field copy of `providers.ProviderPolicy`),
  `ProviderSetupItem` (built by two twin sources, `providerDraftItemSource` /
  `providerConfigItemSource`), `ProviderFormSpecInput`, `ProviderFormState`.
  Validation is duplicated in `buildProviderConfig`,
  `applyProviderSetupUpdate`, `ProviderFormState.ValidationMessage` and the
  controlplane's `providerEditValidationMessage`.
- Presentation in the persistence package: `provider_form.go`,
  `provider_form_state.go`, `provider_form_view.go` (field specs, choices,
  labels, CLI command ids "base"/"key"/"tools"), `provider_view.go` and
  `provider_model_catalog.go` (display strings).
- Built-in defaults are written into setup.json: `normalizeProviderConfig`
  copies name, type, `api_key_env`, base URL, default model and default
  reasoning effort of catalog providers; `normalizeVoiceModuleConfig` adds an
  entry with model/voice/language/binary/endpoint defaults for every local
  voice provider and fills the default provider id; browser provider id and
  runtime mode; assistant name "matrixclaw". Defaults then never change for
  existing installs.
- "-" clear sentinels inside setup (`mergeTelephonyConfig`,
  `mergeWebSearchConfig`); the web search update type is the stored config
  itself, and the GET/PATCH response returns the raw Tavily/Serper keys.
- Controlplane provider forms (`provider_custom_*.go`, ~1.1k lines) carry the
  whole form state, API key included, in an encoded token inside every
  command string; Telegram keeps those strings in its callback map. The voice
  provider form (`modules_voice_provider_form.go`, 265 lines) is unreachable:
  `setupProviderIDForVoiceProvider` always returns "".

## Target shape

### `internal/setup`: one typed config, edited in place

```go
func (s *Service) Load() (Config, error)
// Update loads the file, applies change, validates and saves atomically
// under the service lock.
func (s *Service) Update(change func(*Config) error) (Config, error)
```

- `Config.Validate() error`: structural checks only (timezone parses,
  Telegram token and numeric user id when enabled, provider ids unique, each
  provider valid, active provider exists). No network, no env lookups, so a
  voice edit never fails because of Telegram or a missing env key.
- One provider representation, `ProviderConfig` + `providers.ProviderPolicy`:
  - stored fields are only what the user chose; `catalog_id` is dropped (it
    always equalled `id`), and `name`, `type`, `api_key_env`, `base_url` equal
    to the catalog's values are not written;
  - `(ProviderConfig) Effective() ProviderConfig` fills catalog defaults
    (name, type, env name, base URL, model, model-default reasoning effort);
  - `(ProviderConfig) Runtime() (ProviderConfig, bool)` = effective + API key
    from the file or env (replaces `ProviderConfigWithResolvedAPIKey`);
  - `CheckProvider(p) error` = `validate` + required key present; used by the
    API and the wizard before saving a provider.
- Provider operations as `Update` closures, no `Draft`:
  `ConfigureProvider(ctx, id, ProviderSetupUpdate) (ProviderSetupItem, error)`,
  `DeleteProvider(ctx, id)`, `ProviderModelCatalog(ctx, ProviderConfig)` and
  `ProviderModelCatalogFor(ctx, id, ProviderSetupUpdate)` (pending form state,
  same apply rules, nothing saved). The "changing base URL requires the key
  again" guard stays in the one `applyProviderUpdate`.
- `ProviderItems(cfg Config) []ProviderSetupItem`: one builder for the HTTP
  list, CLI and wizard (configured: active first; then implemented catalog
  providers as `ProviderConfig{ID: catalogID}.Effective()`). `ProviderOption`
  goes; the catalog is `providers.ProviderSpecs` / `PolicyForProvider`.
- `Apply(ctx, edited Config) (ApplyResult, error)` for the wizard: validates,
  checks the Telegram token (`getMe`, only here), then under the lock copies
  the sections the wizard owns (assistant, providers, active provider, daemon
  address/db/timezone/autostart, clients) onto the current file, keeping
  budgets, modules and the API token, and installs/starts the daemon.
- `Store` interface and the draft file go; `FileStore` is Load/Save/Path.
  `Load` and `Save` share one `canonical(cfg)` (trim, persisted-value aliases
  until phase D, dedupe, strip defaults). Voice and browser descriptors keep
  computing effective values (`voiceProviderConfigByID` etc.); the voice
  update path stores only fields that differ from the provider's defaults.
- Explicit updates (HTTP contract): `TelephonyModuleUpdate` and a new
  `WebSearchConfigUpdate` use pointer fields (absent = unchanged, `""` =
  clear); `clear_token` and every `"-"` check leave setup. The web search
  response carries `tavily_key_preview`/`serper_key_preview` instead of keys.
  Request-only voice module id aliases ("live", "text-to-speech", ...) go.
- `ProviderSetupUpdate` fields become pointers (`*string`, `*int`,
  `*ToolUseMode`); JSON keys and `omitempty` unchanged, so Swift
  (`ProviderSetupUpdate` with optionals) needs no change. New meaning of an
  explicit `""`: reset to the built-in default (`base_url`, `model`,
  `reasoning_effort`, `tool_use_mode`); no client sends `""` today.
  `ProviderSetupItem` JSON is unchanged (Swift decodes it).

### Presentation moves to consumers

- `internal/controlplane/provider_form.go` (new, replaces
  `provider_custom_create/edit/fields.go`): one form for built-in, configured
  and new custom providers. Form state (`providerForm`: provider id/type,
  custom/new flags, name, base URL, model, key, effort, tool mode) lives in a
  package-level store keyed by a random 128-bit id with a 30 min TTL and a
  size cap; commands are `/provider form <id> [field <f> | set <f> <value> |
  save]`. The key is only ever in the one command that submits it; prompts
  for the key are never prefilled. Field rows, choices and labels are built
  here from `ProviderSetupItem` + `providers` policy/capabilities. The extra
  confirm step for custom providers goes (Save is explicit).
- `clients/terminal/setup`: the wizard edits a typed `setup.Config` (bool
  pickers set bools, the provider form edits a stored `ProviderConfig` and
  shows `Effective()` values), keeps an in-memory snapshot for cancel, and
  calls `service.Apply(ctx, cfg)` at the end. Its provider rows come from
  policy/capabilities in `model_provider_form.go`.
- `internal/clientcmd`: `providers` / `providers verify` / `doctor` use
  `setup.ProviderItems(cfg)` and `ProviderModelCatalogFor`.

## Deleted

`Draft`, `ProviderDraft`, `ProviderOption`, draft store
(`LoadDraft/SaveDraft/ClearDraft`, `ErrDraftNotFound`, `Store`),
`buildConfig`, `buildProviderConfig`, `draftFromConfig`, `defaultDraft`,
`SummaryFromDraft`, `BoolString`, `ParseBool`, the item-source twins,
`provider_form*.go`, `provider_view.go`, `provider_model_catalog.go`,
`ProviderModels`, `catalog_id` key, `modules_voice_provider_form.go` and its
dispatch cases, the encoded form token and its decoders.
Rough size: about -2600 / +1100 lines.

## Persisted data

No version bump. Old files load unchanged: `catalog_id` is ignored, persisted
defaults are equal to the defaults and are stripped on the next save. New
files are readable by older binaries (they re-fill defaults on load). A
leftover `setup.json.draft` is ignored. Behaviour change: leaving the wizard
half-way no longer resumes unsaved edits next time; picking a model in a
session no longer contacts Telegram.

## Commits (each green)

1. `refactor(setup)`: `Update` exported, `ProviderConfig.Effective/Runtime`,
   `CheckProvider`, `ProviderItems`, provider operations as `Update` closures
   with pointer `ProviderSetupUpdate`; API, daemonclient, clientcmd,
   controlplane call sites follow. Wizard still on `Draft`.
2. `refactor(setup)`: wizard on typed `Config` + `Service.Apply(ctx, Config)`;
   delete `Draft` and the draft store.
3. `refactor(controlplane)`: provider forms by opaque id, field specs out of
   setup; delete the token, setup form/view files and the dead voice form.
4. `refactor(setup)`: built-in defaults not persisted (providers, voice,
   browser, assistant name); `Effective` used by daemoncmd/clientcmd.
5. `refactor(setup)`: explicit telephony/web search updates, secret-free web
   search response, request-only voice id aliases removed.
6. `docs`: ARCHITECTURE + as-built notes.

## Risks and tests

- Config loss on partial edits (the #1 bug class): tests that a provider
  update, a model switch and a wizard `Apply` keep budgets, modules, API token
  and other providers; concurrent `Update` test stays.
- Defaults: round-trip test that an old file with persisted defaults loads
  with the same effective values and saves without them; built-in provider
  with empty model runs on the catalog default.
- Provider rules: tests for the base-URL/key guard, custom provider
  validation, active provider selection on add/delete.
- Controlplane forms: tests that no command string contains the key, that a
  form edit/save round-trip sends the right update, and that an expired id
  answers with a reopen hint.
- TUI wizard: model tests for provider save validation and bool editing;
  manual run of `matrixclaw setup` against a temp `MATRIXCLAW_SETUP_PATH`.

## As built

- Steps 1 and 2 landed as one commit: the wizard and the API paths shared the
  `Draft` plumbing too closely to keep both green separately. Step 6 is a
  small extra cleanup of provider helpers that only the old forms used.
- `Service.Update` canonicalizes the config (trim, persisted aliases, strip
  defaults) before `Config.Validate`; `Apply` uses the same path, creating
  the file on first run. `CheckProvider` (structure plus a usable key) runs
  in `ConfigureProvider` and the wizard, not in `Validate`, so an env key that
  disappears never blocks unrelated edits.
- `ProviderModelCatalog` is a function of a `ProviderConfig` and returns the
  response only; `ProviderModelCatalogFor` applies pending form edits with
  the same base-URL/key guard as saving.
- Stored defaults dropped on save: provider `name`, `type`, `api_key_env`,
  `base_url` equal to the catalog's, `catalog_id`; assistant name
  "matrixclaw"; browser `provider_id` "playwright" and `runtime_mode`
  "per_task"; local voice model/voice/language/binary/endpoint/runtime mode
  equal to the defaults and voice entries left empty by that. Provider model
  and reasoning effort are kept as stored (they can be user choices); cloud
  realtime voice providers keep their endpoint and key env as before, since
  the daemon's env overrides read them.
- Controlplane provider forms: `/provider form <id> [field|set|save]`, form
  state is a `setup.ProviderSetupUpdate` over the provider's
  `ProviderSetupItem` in a 30 min / 64 entry store; a failed save keeps the
  form open with the daemon's error. Typing `-` in a telephony or web search
  prompt still clears the value (now sent as an explicit empty field).
- The unconfigured provider items now carry `api_key_preview` too (e.g.
  `env:OPENAI_API_KEY`), so a form for a provider with an env key can load
  models and save without retyping it.
