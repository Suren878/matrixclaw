# Image Generation

The `image` module gives the assistant a `generate_image` tool. It works with
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
  "image": {"enabled": true, "provider": "foresko", "model": "qwen-image", "size": "1024x1024"}
}
```

## `generate_image`

```text
prompt  what the image shows (required)
size    optional WIDTHxHEIGHT for this image
```

- Each call asks for approval unless a permission rule allows it
  (`generate_image` in `/permissions`).
- One image per call. The answer may carry base64 or a URL; a URL is
  downloaded at once. Requests time out after 5 minutes, images are at most
  50 MB.
- The image is saved to temporary storage under `images/`; the result names
  the path, so the assistant can keep it with `storage_save` or send it again
  with `send_file`.
- In a Telegram chat it is also sent as a photo; one larger than Telegram's
  10 MB photo limit goes as a document.
