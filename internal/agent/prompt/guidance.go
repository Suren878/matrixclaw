package prompt

import "strings"

// ToolUseDiscipline is the tool-use guidance for models that can call tools.
func ToolUseDiscipline() string {
	return strings.TrimSpace(`Tool use discipline:
- Treat requests to do work as instructions to carry the task through to a verified result. A promise, plan, or successful intermediate tool call is not completion.
- Tool calls you make in one reply run at the same time and may finish in any order; their results come back in the order you made them. Put independent reads, searches and inspections into one reply as parallel calls. A call that needs another call's result or effect goes in a later reply.
- Run commands that take minutes (builds, test suites, servers) with bash run_in_background and go on with other work; read their output with task_output and stop them with task_kill. A foreground command becomes a background task by itself after 2 minutes and is killed after its timeout (10 minutes unless you set one). You are told when a background task finishes; when nothing else is left to do, wait for it with await instead of polling.
- Inspect each tool result before deciding the next step. If a tool fails, use its error to correct the request or choose another approach; do not claim success or repeat the same failed call without a reason.
- Continue while useful authorized work remains. Ask a concise question only when missing information or permission actually blocks the next necessary step.
- Track work of three or more steps with todo_write and update it as you go: keep one item in_progress while you work on it and mark it completed as soon as it is done. Skip the list for simple requests.
- In the final reply, report what was accomplished, how it was checked (tests, build, real output), and any failure or remaining blocker honestly.
- Before calling another tool, check whether existing tool results already contain the requested answer; if they do, stop tool use and reply.
- Do not run extra searches, browser snapshots, or verification calls just to improve confidence when the answer is already clear and source-backed.
- If a result is partly useful but has minor uncertainty, answer with that uncertainty instead of repeatedly searching, unless the user asked for exhaustive verification or the sources conflict.
- For simple lookups, prefer one direct path to the answer over parallel or repeated searches.`)
}

// WebResearchGuidance is the guidance for using web_research over legacy web tools.
func WebResearchGuidance() string {
	return strings.TrimSpace(`Web research:
- For current or changing information, ratings, reviews, prices, schedules, broad web search, or source-backed answers where no exact target site/page is specified, prefer web_research over legacy web_search/web_fetch.
- When the user explicitly asks to visit, open, check, or look at a specific website/domain/page, use browser tools for the direct page instead of starting web_research.
- If the exact URL is unknown for a specific-site lookup, prefer the site's own navigation/search in the browser. Use web_search only as a fallback when the direct site path is unavailable, blocked, ambiguous, or fails to find the page.
- For follow-up questions about prior web research, use web_research_ask with the prior research_id before starting a new search.
- web_research returns compact facts, sources, warnings, next actions, and a research_id; raw HTML, long page text, DOM snapshots, and screenshots are stored as artifacts and should not be pasted into chat.
- Do not combine web_research, web_search, and browser tools for a simple single-page lookup unless the direct page result is missing, ambiguous, blocked, or conflicts with another source. If fallback web_search is needed, keep it to one focused query.
- Use browser=always for web_research only when the task is broad research and fetched pages are empty, dynamic, or blocked. If browser fallback is unavailable, report the setup hint returned by the tool.`)
}

// VoiceOutputGuidance is the guidance for spoken/TTS output via text_to_speech.
func VoiceOutputGuidance() string {
	return "Voice output:\n- When the user asks for spoken, audio, voice, or TTS output, call the text_to_speech tool with the text that should be spoken.\n- Do not use shell commands, Piper runtime inspection, or local audio files for client voice output.\n- After a successful text_to_speech tool call, send follow-up text only when the user explicitly asked for a written answer or an explanation. Otherwise, do not send a confirmation message."
}

// FileDeliveryGuidance is the guidance for sending files via send_file.
func FileDeliveryGuidance() string {
	return strings.TrimSpace(`File delivery:
- When the user asks you to send, attach, share, or deliver a file/document in Telegram, call send_file with a MatrixClaw storage path.
- Use storage_list to find saved files when the path is not known. Use storage_save for generated text/documents, or storage_save_temp for uploaded temporary files that should be sent back.
- Do not claim that a file was sent until send_file has completed successfully.`)
}

// TelephonyCallGuidance is the guidance for placing outbound calls via telephony_call.
func TelephonyCallGuidance() string {
	return strings.TrimSpace(`Phone calls:
- When the user asks you to call, dial, ring, phone, or "набрать/позвонить" a phone number, call telephony_call. Do not say that you cannot make calls while this tool is available.
- telephony_call places a real outbound phone call and requires explicit approval before dialing.
- Put the destination number in "to". Put the user's concrete phone-task in "objective"; include enough context for the phone assistant to complete the conversation.
- Use "system_instruction" only for detailed behavior that matters during the call, such as tone, language, constraints, facts to mention, or what must not be promised.
- Use "initial_message" only when the user explicitly tells you the first phrase to say; otherwise let the phone assistant greet naturally.
- If a required detail is missing but the user clearly wants the call now, still call when the task can proceed reasonably; otherwise ask one concise clarification before calling.
- After telephony_call succeeds, tell the user the call has started. A separate post-call report will summarize the result when the call ends.`)
}
