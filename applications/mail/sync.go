package mail

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/governance"
	"horizon/internal/repository/dbgen"
	"horizon/internal/repository/dbgen/maildb"
)

func clip(value string, limit int) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) > limit {
		return strings.ToValidUTF8(value[:limit], "")
	}
	return value
}

func (m *Module) syncInbox(protocol string) func(context.Context, pgx.Tx, contracts.Input) (contracts.Output, error) {
	return func(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
		var cleanups []func(context.Context) error
		output, err := m.syncInboxTx(ctx, tx, in, protocol, &cleanups)
		if err != nil {
			cancelContext := context.WithoutCancel(ctx)
			for _, cleanup := range cleanups {
				if cleanupErr := cleanup(cancelContext); cleanupErr != nil {
					governance.LogStorageFailure(cleanupErr)
				}
			}
			return contracts.Output{}, err
		}
		return output, nil
	}
}

func (m *Module) syncInboxTx(ctx context.Context, tx pgx.Tx, in contracts.Input, protocol string, cleanups *[]func(context.Context) error) (contracts.Output, error) {
	system, err := requireSystem(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	var payload struct {
		ChannelID int64 `json:"channelId"`
	}
	if err = contracts.Bind(in, &payload); err != nil {
		return contracts.Output{}, err
	}
	q := dbgen.New(tx)
	receive := maildb.New(tx)
	channel, err := receive.ReceiveDue(ctx, maildb.ReceiveDueParams{
		TenantID: system.TenantID, ID: payload.ChannelID, Protocol: protocol,
	})
	if err != nil {
		return contracts.Output{}, problem(err)
	}
	lease, err := governance.Token()
	if err != nil {
		return contracts.Output{}, err
	}
	leased, err := q.MailReceiveLease(ctx, dbgen.MailReceiveLeaseParams{LeaseOwner: lease, TenantID: channel.TenantID, ID: channel.ChannelID})
	if err != nil {
		return contracts.Output{}, problem(err)
	}
	if leased != 1 {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	password, err := openCredential(m.Core, channel.TenantID, channel.MailboxID, receiveCredentialPurpose, channel.Version, channel.Ciphertext)
	if err != nil {
		return contracts.Output{}, contracts.Problem("unavailable")
	}
	batch, err := m.receiveBatch(ctx, receive, channel, password)
	if err != nil {
		return contracts.Output{}, contracts.Problem("unavailable")
	}
	defer batch.close()
	syncID, err := q.MailInboxSyncCreate(ctx, dbgen.MailInboxSyncCreateParams{TenantID: channel.TenantID, MailboxID: channel.MailboxID})
	if err != nil {
		return contracts.Output{}, problem(err)
	}
	events := []contracts.Event{}
	fetched, imported, duplicates := 0, 0, 0
	for _, item := range batch.items {
		if item.uid == "" || len(item.uid) > 255 || strings.ContainsAny(item.uid, "\r\n ") {
			return contracts.Output{}, contracts.Problem("invalid")
		}
		fetched++
		exists, err := q.MailMessageUIDExists(ctx, dbgen.MailMessageUIDExistsParams{
			TenantID: channel.TenantID, MailboxID: channel.MailboxID, ProviderUid: item.uid,
		})
		if err != nil {
			return contracts.Output{}, err
		}
		if exists {
			duplicates++
			continue
		}
		entity, err := batch.fetch(item.id)
		if err != nil {
			return contracts.Output{}, contracts.Problem("unavailable")
		}
		if entity == nil {
			continue
		}
		parsed, err := ParseMIME(entity, m.options.MaxMessageBytes)
		if err != nil {
			return contracts.Output{}, contracts.Problem("invalid")
		}
		if len(parsed.Attachments) > m.options.MaxAttachments {
			return contracts.Output{}, contracts.Problem("invalid")
		}
		message, err := q.MailMessageCreate(ctx, dbgen.MailMessageCreateParams{
			TenantID: channel.TenantID, MailboxID: channel.MailboxID, OwnerAccountID: channel.OwnerAccountID,
			Direction: "inbound", Folder: "inbox", ProviderUid: item.uid,
			RfcMessageID: clip(parsed.RFCMessageID, 998), Subject: clip(parsed.Subject, 998),
			FromAddress: clip(parsed.From, 320), ReplyTo: clip(parsed.ReplyTo, 320),
			BodyText: parsed.Text, BodyHtml: parsed.HTML, Snippet: Snippet(parsed.Text),
			OccurredAt: pgtype.Timestamptz{Time: parsed.OccurredAt.UTC(), Valid: true},
		})
		if err != nil {
			return contracts.Output{}, problem(err)
		}
		if !message.Inserted {
			duplicates++
			continue
		}
		imported++
		for _, list := range []struct {
			kind string
			rows []mail.Address
		}{{"to", parsed.To}, {"cc", parsed.CC}} {
			for _, recipient := range list.rows {
				if err = q.MailMessageRecipientsAdd(ctx, dbgen.MailMessageRecipientsAddParams{
					TenantID: channel.TenantID, MessageID: message.ID, Kind: list.kind,
					Address: clip(recipient.Address, 320), DisplayName: clip(recipient.Name, 200),
				}); err != nil {
					return contracts.Output{}, problem(err)
				}
			}
		}
		if len(parsed.Attachments) > 0 {
			service, err := m.fileService()
			if err != nil {
				return contracts.Output{}, err
			}
			owner, err := m.Identity.PrincipalForTask(ctx, channel.TenantID, channel.OwnerAccountID)
			if err != nil {
				return contracts.Output{}, problem(err)
			}
			for _, attachment := range parsed.Attachments {
				if attachment.Filename == "" || attachment.MIME == "" || len(attachment.Data) > m.options.MaxAttachmentBytes {
					return contracts.Output{}, contracts.Problem("invalid")
				}
				file, cleanup, err := service.CreateBoundInTx(ctx, tx, owner, clip(attachment.Filename, 255), attachment.MIME, "mail_attachment", attachment.Data)
				if err != nil {
					return contracts.Output{}, problem(err)
				}
				*cleanups = append(*cleanups, cleanup)
				if _, err = q.MailMessageAttachmentAdd(ctx, dbgen.MailMessageAttachmentAddParams{
					TenantID: channel.TenantID, MessageID: message.ID, FileID: file.ID,
					ContentID: clip(attachment.ContentID, 255), Filename: file.Name,
					MimeType: file.Mime, Size: file.Size, Disposition: attachment.Disposition,
				}); err != nil {
					return contracts.Output{}, problem(err)
				}
			}
		}
		events = append(events, mailEvent(MessageReceivedEvent, "mail_message", message.ID, map[string]any{
			"mailboxId": channel.MailboxID, "messageId": message.ID,
		}))
	}
	next := time.Now().Add(time.Minute)
	if batch.more {
		next = time.Now().Add(5 * time.Second)
	}
	if batch.cursor != nil {
		if err = receive.IMAPCursorSave(ctx, maildb.IMAPCursorSaveParams{
			ImapUidValidity: batch.cursor.validity, ImapLastUid: batch.cursor.last, TenantID: channel.TenantID, ID: channel.ChannelID,
		}); err != nil {
			return contracts.Output{}, problem(err)
		}
	}
	if _, err = q.MailReceiveRelease(ctx, dbgen.MailReceiveReleaseParams{
		NextSyncAt: pgtype.Timestamptz{Time: next, Valid: true}, TenantID: channel.TenantID, ID: channel.ChannelID,
	}); err != nil {
		return contracts.Output{}, err
	}
	if _, err = q.MailInboxSyncFinish(ctx, dbgen.MailInboxSyncFinishParams{
		Status: "completed", FetchedCount: int32(fetched), ImportedCount: int32(imported),
		DuplicateCount: int32(duplicates), ErrorCode: "", TenantID: channel.TenantID,
		ID: syncID, MailboxID: channel.MailboxID,
	}); err != nil {
		return contracts.Output{}, err
	}
	events = append(events, mailEvent(MailboxSyncedEvent, "mail_mailbox", channel.MailboxID, map[string]any{
		"mailboxId": channel.MailboxID, "fetched": fetched, "imported": imported, "duplicates": duplicates,
	}))
	return contracts.Output{
		Data: map[string]any{"mailboxId": channel.MailboxID, "fetched": fetched,
			"imported": imported, "duplicates": duplicates},
		Events: events,
	}, nil
}

type inboxItem struct {
	id  int64
	uid string
}

type inboxBatch struct {
	items  []inboxItem
	more   bool
	fetch  func(int64) (*message.Entity, error)
	close  func()
	cursor *imapBatch
}

func (m *Module) receiveBatch(ctx context.Context, q *maildb.Queries, channel maildb.ReceiveDueRow, password string) (inboxBatch, error) {
	if channel.Protocol == "pop3" {
		return m.pop3Batch(ctx, q, channel, password)
	}

	config := ReceiveInput{Protocol: channel.Protocol, Host: channel.Host, Port: int(channel.Port), TLSMode: channel.TlsMode, Username: channel.Username}
	connection, err := imapConnect(ctx, config, password, m.options.AllowedEndpoints)
	if err != nil {
		return inboxBatch{}, err
	}
	cursor, err := connection.batch(channel.ImapUidValidity, channel.ImapLastUid, m.options.SyncBatch, m.options.MaxMessageBytes)
	if err != nil {
		connection.close()
		return inboxBatch{}, err
	}
	batch := inboxBatch{close: connection.close, items: make([]inboxItem, 0, len(cursor.uids)), more: cursor.more, cursor: &cursor,
		fetch: func(uid int64) (*message.Entity, error) {
			return connection.fetch(imap.UID(uid), m.options.MaxMessageBytes)
		}}
	for _, uid := range cursor.uids {
		batch.items = append(batch.items, inboxItem{id: int64(uid), uid: imapProviderUID(config, cursor.validity, uid)})
	}
	return batch, nil
}
