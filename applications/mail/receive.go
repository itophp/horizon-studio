package mail

import (
	"context"

	"horizon/foundation/kernel/capability/contracts"
)

// The receiving credential is shared across protocol changes. Its persisted
// purpose is also the authenticated-data key used by existing POP3 mailboxes.
const receiveCredentialPurpose = "pop3"

func (in *ReceiveInput) normalize(email string) error {
	if in.Protocol == "" {
		if in.Host != "" || in.Port != 0 || in.TLSMode != "" || in.Username != "" {
			return contracts.Problem("invalid")
		}
		in.Protocol, in.Host, in.Port, in.TLSMode, in.Username = "pop3", in.POP3Host, in.POP3Port, "tls", in.POP3Username
	} else if in.POP3Host != "" || in.POP3Port != 0 || in.POP3Username != "" {
		return contracts.Problem("invalid")
	}
	if in.TLSMode == "" {
		in.TLSMode = "tls"
	}
	validMode := in.TLSMode == "tls" || (in.Protocol == "imap" && in.TLSMode == "starttls")
	if (in.Protocol != "pop3" && in.Protocol != "imap") || !validMode ||
		in.Host == "" || in.Port < 1 || in.Port > 65535 || !headerSafe(in.Host, in.Username) {
		return contracts.Problem("invalid")
	}
	if in.Username == "" {
		in.Username = email
	}
	return nil
}

func (m *Module) testReceiver(ctx context.Context, config ReceiveInput, password string) error {
	if config.Protocol == "pop3" {
		return testPOP3(ctx, config.Host, config.Port, config.Username, password, m.options.AllowedEndpoints)
	}
	connection, err := imapConnect(ctx, config, password, m.options.AllowedEndpoints)
	if err != nil {
		return err
	}
	defer connection.close()
	_, err = connection.inbox()
	return err
}
