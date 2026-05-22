package setup

import (
	"strings"
	"testing"
)

func TestDefaultAssistantSystemPromptMentionsSkillsCompactly(t *testing.T) {
	prompt := DefaultAssistantSystemPrompt()
	for _, want := range []string{"skill_search", "skill_use", "skill_manage", "explicit user confirmation"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("DefaultAssistantSystemPrompt() missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "call the text_to_speech tool") {
		t.Fatalf("default prompt still duplicates voice guidance:\n%s", prompt)
	}
}

func TestInitializeAssistantSystemPromptReplacesLegacyDefaultOnly(t *testing.T) {
	legacy := "You are matrixclaw, a personal AI operator running through matrixclaw's local background runtime. You work across terminal and Telegram clients on durable sessions with files, deliveries, provider/model selection, explicit tool approvals, reminders, scheduled AI tasks, optional text-to-speech and speech-to-text modules, and optional external-agent sessions. Reply in the same language the user uses for the current request; if the user mixes languages, use the language that best matches the user's latest request. Use the actual tools made available in each turn; plan tools can read and update the visible session goal and plan for multi-step work. When the user asks for spoken, audio, voice, or TTS output, call the text_to_speech tool with the text that should be spoken; do not use shell commands, Piper runtime inspection, or local audio files for client voice output. The client control-plane also has slash commands for session management, providers, permissions, context, token usage, plan, search, modules, tasks, and server actions; explain those commands when useful, but do not claim you can run a control-plane command unless it is exposed as a tool. Prefer precise actions, preserve user files, ask for approval before risky or destructive changes, keep responses concise, and update the session plan as work progresses. In user-facing explanations, call the background runtime matrixclaw architect instead of daemon. When users ask for reminders or scheduled work, resolve exact time and timezone before creating automation."
	updated := InitializeAssistantSystemPromptWithContext(legacy, AssistantPromptContext{})
	if !strings.HasPrefix(updated, DefaultAssistantSystemPrompt()) || strings.Contains(updated, "call the text_to_speech tool") {
		t.Fatalf("legacy default was not replaced:\n%s", updated)
	}

	custom := "Custom prompt. call the text_to_speech tool only if I say voice."
	if got := InitializeAssistantSystemPromptWithContext(custom, AssistantPromptContext{}); !strings.HasPrefix(got, custom) {
		t.Fatalf("custom prompt was changed: %q", got)
	}
}

func TestProjectContextMentionsVoiceAndCoreCapabilitiesCompactly(t *testing.T) {
	context := compactProjectContext(AssistantPromptContext{})
	for _, want := range []string{
		"tools=files,shell,web,storage,automation,tts,skills,mcp_when_enabled",
		"voice=tts_output_tool_when_available;stt_transcribes_user_speech_before_chat",
		"approvals=write_shell_skill_manage_and_risky_tools_need_permission",
	} {
		if !strings.Contains(context, want) {
			t.Fatalf("compactProjectContext() missing %q:\n%s", want, context)
		}
	}
	if strings.Contains(context, "/sessions ->") || strings.Contains(context, "plan_guidance=") {
		t.Fatalf("compactProjectContext() regressed to verbose command guidance:\n%s", context)
	}
}
