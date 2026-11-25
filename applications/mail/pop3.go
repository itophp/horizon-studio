package mail

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/emersion/go-message"
	"github.com/knadh/go-pop3"
	"horizon/foundation/system/governance"
	"horizon/internal/repository/dbgen/maildb"
)

func pop3Connect(ctx context.Context, host string, port int, username, password string, allowed []string) (*pop3.Conn, error) {
	if !allowedEndpoint(host, port, allowed) {
		return nil, governance.Problem("forbidden")
	}
	client := pop3.New(pop3.Opt{Host: host, Port: port, TLSEnabled: true, DialTimeout: 10 * time.Second})
	connection, err := client.NewConn()
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*pop3.Conn, error) {
		_ = connection.Quit()
		return nil, err
	}
	if username != "" {
		if err = connection.Auth(username, password); err != nil {
			return fail(err)
		}
	} else {
		if err = connection.Auth("", password); err == nil {
			return fail(errors.New("pop3_username_required"))
		}
		return fail(governance.Problem("invalid"))
	}
	if _, _, err = connection.Stat(); err != nil {
		return fail(err)
	}
	return connection, nil
}

func testPOP3(ctx context.Context, host string, port int, username, password string, allowed []string) error {
	connection, err := pop3Connect(ctx, host, port, username, password, allowed)
	if err != nil {
		return err
	}
	if err = connection.Noop(); err != nil {
		_ = connection.Quit()
		return err
	}
	return connection.Quit()
}

func (m *Module) pop3Batch(ctx context.Context, q *maildb.Queries, channel maildb.ReceiveDueRow, password string) (inboxBatch, error) {
	connection, err := pop3Connect(ctx, channel.Host, int(channel.Port), channel.Username, password, m.options.AllowedEndpoints)
	if err != nil {
		return inboxBatch{}, err
	}
	uids, err := connection.Uidl(0)
	if err != nil {
		_ = connection.Quit()
		return inboxBatch{}, err
	}
	items, err := pendingPOP3Items(ctx, q, channel, uids, m.options.SyncBatch+1)
	if err != nil {
		_ = connection.Quit()
		return inboxBatch{}, err
	}
	limit := min(len(items), m.options.SyncBatch)
	return inboxBatch{
		items: items[:limit], more: len(items) > limit,
		fetch: func(id int64) (*message.Entity, error) { return connection.Retr(int(id)) },
		close: func() { _ = connection.Quit() },
	}, nil
}

func pendingPOP3Items(ctx context.Context, q *maildb.Queries, channel maildb.ReceiveDueRow, remote []pop3.MessageID, limit int) ([]inboxItem, error) {
	items := make([]inboxItem, 0, limit)
	for group := range slices.Chunk(remote, 1000) {
		uids := make([]string, 0, len(group))
		ids := make(map[string]int, len(group))
		for _, item := range group {
			uids = append(uids, item.UID)
			ids[item.UID] = item.ID
		}
		pending, err := q.PendingPOP3UIDs(ctx, maildb.PendingPOP3UIDsParams{
			TenantID: channel.TenantID, MailboxID: channel.MailboxID, Uids: uids, MaxRows: int32(limit - len(items)),
		})
		if err != nil {
			return nil, err
		}
		for _, uid := range pending {
			items = append(items, inboxItem{id: int64(ids[uid]), uid: uid})
		}
		if len(items) == limit {
			break
		}
	}
	return items, nil
}
