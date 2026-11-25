package mail

import "horizon/foundation/kernel/applicationruntime"

// RegisterUIContract contributes the form and message content slots used by the
// declarative pages. Provider and action bindings remain application metadata.
func RegisterUIContract() {
	applicationruntime.RegisterUIContracts(
		messageListUIContract(),
		mailComponent("mail-compose-form", "Compose fields and local attachment selection. Reads authorized sender choices from its bound Provider.", "submit", "sent", "suggest-draft"),
		mailComponent("mail-mailbox-form", "Mailbox connection and credential fields with ephemeral submission.", "connect", "update", "replace", "close", "refresh", "feedback"),
		mailComponent("mail-message-content", "Message headers, sandboxed body, reply preparation and private attachment download.", "read", "analyze", "translate"),
	)
}

func messageListUIContract() applicationruntime.UIContract {
	contract := mailComponent("mail-message-list", "Message summaries, read state and page navigation for one folder.", "open", "recommend")
	contract.PropsSchema = `{"type":"object","properties":{"folder":{"type":"string","enum":["inbox","sent"]}},"required":["folder"],"additionalProperties":false}`
	contract.Example = `{"type":"component","component":{"id":"mail-messages","kind":"mail-message-list","version":"1.0","props":{"folder":"inbox"}}}`
	return contract
}

func mailComponent(name, description string, events ...string) applicationruntime.UIContract {
	return applicationruntime.UIContract{
		Name: name, Version: "1.0", Category: "element", Description: description,
		PropsSchema: `{"type":"object","additionalProperties":false}`,
		Events:      events, Slots: []string{}, Binding: "data",
		ReadOnly: "Disable mutations and local file operations.",
		Example:  `{"type":"component","component":{"id":"` + name + `","kind":"` + name + `","version":"1.0","props":{}}}`,
	}
}
