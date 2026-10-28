# Images

The assistant draws pictures and edits the ones you send: "draw a lighthouse
at night", or a photo with "dress him as a king". The `image` module gives it
the `generate_image` tool, which works with
any configured provider that serves the OpenAI images API
(`POST <base_url>/images/generations`): OpenAI, xAI, a self-hosted
OpenAI-compatible server, and Gemini through its `/openai` path. Anthropic and
Codex providers generate no images and are not offered.

## Setup

`/modules` -> Image generation:

- **Provider**: one of the configured providers. Its API key and base URL are
  used as they are; the module stores no key of its own.
- **Model**: picked from the provider's `/models` list, or typed when the
  provider does not list its models. It is independent of the chat model, so
  one provider can chat with one model and draw with another.
- **Size**: `WIDTHxHEIGHT` sent with every request; empty leaves the size to
  the provider.
- **Enabled**: the tool is offered only while the module is on and the
  provider has its key.

In setup.json:

```json
"modules": {
  "image": {"enabled": true, "provider": "openai", "model": "gpt-image-1", "size": "1024x1024"}
}
```

## `generate_image`

```text
prompt  what the image shows, or what to change (required)
size    optional WIDTHxHEIGHT for this image
images  optional storage paths of up to 4 images to edit or use as references
```

- Without `images` the call goes to `images/generations` and draws from
  scratch. With them it goes to `images/edits` as a multipart form (`image`,
  or `image[]` for several), the way the OpenAI API takes edits. A provider
  without that endpoint answers with an error saying so; the assistant does not
  fall back to drawing from scratch.
- Paths are read from temporary storage, else from saved storage, so a photo
  the user attached in Telegram or an image generated earlier can be edited by
  a chat model that cannot see images itself.
- Each call asks for approval unless a permission rule allows it
  (`generate_image` in `/permissions`).
- One image per call. The answer may carry base64 or a URL; a URL is
  downloaded at once. Requests time out after 5 minutes, images are at most
  50 MB.
- The image is saved to temporary storage under `images/`; the result names
  the path, so the assistant can keep it with `storage_save_temp` or send it again
  with `send_file`.
- In a Telegram chat it is also sent as a photo; one larger than Telegram's
  10 MB photo limit goes as a document.

## Photos you send

A photo sent in Telegram is saved to temporary storage under
`telegram/images/` and attached to your message.

- A chat model with image input sees the photo itself.
- A model without image input does not see it, but its message names the
  file's path, so the model can still hand the photo to a tool: edit it with
  `generate_image` (`images`), save it with `storage_save_temp`, or send it
  back with `send_file`.
- Editing needs a provider with `images/edits`. Where the image provider has
  none, the assistant says so; with a subagent that can see images it may
  offer a new picture drawn from a description of the photo instead, and says
  that it is not an edit of the original.
