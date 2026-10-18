package controlplane

func customProviderTypePicker() Result {
	return Result{Screen: NewPickerData(PickerProviderCustom, "Custom Provider").
		Back(providerRoot.command()).
		Row("openai", "OpenAI Compatible", "", providerCustomNew.command("openai")).
		Row("anthropic", "Anthropic Compatible", "", providerCustomNew.command("anthropic")).
		Build()}
}
