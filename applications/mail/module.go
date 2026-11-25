package mail

import (
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/kernel/capability/registry"
	"horizon/foundation/system/governance"
)

// Module owns personal external-mail capabilities. The governance core supplies
// the trusted principal, transaction helper and credential root key; the
// business limits are plugin options resolved by the deployment.
type Module struct {
	*governance.Core
	options Options
	gateway *gateway.Gateway
}

func New(core *governance.Core, options Options) *Module {
	if core == nil {
		core = governance.New(nil, nil, governance.Options{})
	}
	return &Module{Core: core, options: options.defaults()}
}

func (m *Module) Entries() []registry.Entry {
	return []registry.Entry{
		{Definition: mailboxListDefinition(), Handler: contracts.Handler{Execute: m.listMailboxes}},
		{Definition: mailboxGetDefinition(), Handler: contracts.Handler{Execute: m.getMailbox}},
		{Definition: mailboxOnboardDefinition(), Handler: contracts.Handler{ExecuteTx: m.onboard}},
		{Definition: mailboxUpdateDefinition(), Handler: contracts.Handler{ExecuteTx: m.updateMailbox}},
		{Definition: credentialReplaceDefinition(), Handler: contracts.Handler{ExecuteTx: m.replaceCredential}},
		{Definition: mailboxEnableDefinition(), Handler: contracts.Handler{ExecuteTx: m.enableMailbox}},
		{Definition: mailboxDisableDefinition(), Handler: contracts.Handler{ExecuteTx: m.disableMailbox}},
		{Definition: connectionTestDefinition(), Handler: contracts.Handler{ExecuteTx: m.testConnection}},
		{Definition: messageListDefinition(), Handler: contracts.Handler{Execute: m.listMessages}},
		{Definition: messageGetDefinition(), Handler: contracts.Handler{Execute: m.getMessage}},
		{Definition: recentAnalysisDefinition(), Handler: contracts.Handler{Execute: m.recommendRecent}},
		{Definition: messageAnalysisDefinition(), Handler: contracts.Handler{Execute: m.analyzeMessage}},
		{Definition: messageTranslationDefinition(), Handler: contracts.Handler{Execute: m.translateMessage}},
		{Definition: messageFlagDefinition(), Handler: contracts.Handler{ExecuteTx: m.updateMessageFlag}},
		{Definition: draftSuggestionDefinition(), Handler: contracts.Handler{Execute: m.suggestDraft}},
		{Definition: messageSendDefinition(), Handler: contracts.Handler{ExecuteTx: m.sendMessage}},
		{Definition: attachmentReadDefinition(), Handler: contracts.Handler{Execute: m.readAttachment}},
		{Definition: deliveryListDefinition(), Handler: contracts.Handler{Execute: m.listDeliveries}},
		{Definition: deliveryRetryDefinition(), Handler: contracts.Handler{ExecuteTx: m.retryDelivery}},
		{Definition: deliveryCancelDefinition(), Handler: contracts.Handler{ExecuteTx: m.cancelDelivery}},
		{Definition: outboundProcessDefinition(), Handler: contracts.Handler{ExecuteTx: m.processOutbound}},
		{Definition: inboxSyncDefinition(), Handler: contracts.Handler{ExecuteTx: m.syncInbox("pop3")}},
		{Definition: imapInboxSyncDefinition(), Handler: contracts.Handler{ExecuteTx: m.syncInbox("imap")}},
	}
}
