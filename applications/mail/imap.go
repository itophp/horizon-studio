package mail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message"
	"horizon/foundation/system/governance"
)

type imapConnection struct {
	client *imapclient.Client
	cancel context.CancelFunc
}

func imapConnect(ctx context.Context, config ReceiveInput, password string, allowed []string) (*imapConnection, error) {
	if !allowedEndpoint(config.Host, config.Port, allowed) {
		return nil, governance.Problem("forbidden")
	}
	if config.Username == "" || (config.TLSMode != "tls" && config.TLSMode != "starttls") {
		return nil, governance.Problem("invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	addr := endpoint(config.Host, config.Port)
	dialer := net.Dialer{Timeout: 10 * time.Second}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		cancel()
		return nil, err
	}
	// Bound the whole session, including reads after the IMAP library changes deadlines.
	context.AfterFunc(ctx, func() { _ = raw.Close() })
	tlsConfig := &tls.Config{ServerName: config.Host, MinVersion: tls.VersionTLS12}
	var client *imapclient.Client
	if config.TLSMode == "starttls" {
		client, err = imapclient.NewStartTLS(raw, &imapclient.Options{TLSConfig: tlsConfig})
	} else {
		secure := tls.Client(raw, tlsConfig)
		err = secure.HandshakeContext(ctx)
		if err == nil {
			client = imapclient.New(secure, nil)
		}
	}
	if err != nil {
		cancel()
		return nil, err
	}
	connection := &imapConnection{client: client, cancel: cancel}
	if err = client.Login(config.Username, password).Wait(); err != nil {
		connection.close()
		return nil, err
	}
	return connection, nil
}

func (c *imapConnection) close() {
	c.cancel()
	_ = c.client.Close()
}

func (c *imapConnection) inbox() (*imap.SelectData, error) {
	data, err := c.client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, err
	}
	if data.UIDValidity == 0 || data.UIDNext == 0 {
		return nil, errors.New("IMAP server did not provide UID validity and next UID")
	}
	return data, nil
}

type imapBatch struct {
	uids     []imap.UID
	validity int64
	last     int64
	more     bool
}

func (c *imapConnection) batch(previousValidity, last int64, batchSize, maxBytes int) (imapBatch, error) {
	mailbox, err := c.inbox()
	if err != nil {
		return imapBatch{}, err
	}
	validity := int64(mailbox.UIDValidity)
	if validity != previousValidity {
		last = 0
	}
	highest := int64(mailbox.UIDNext) - 1
	if last >= highest {
		return imapBatch{validity: validity, last: last}, nil
	}
	// Restrict each search to a bounded UID range even when the server has a large inbox.
	end := min(highest, last+4096)
	var uidRange imap.UIDSet
	uidRange.AddRange(imap.UID(last+1), imap.UID(end))
	result, err := c.client.UIDSearch(&imap.SearchCriteria{
		UID: []imap.UIDSet{uidRange}, Smaller: int64(maxBytes) + 1,
	}, nil).Wait()
	if err != nil {
		return imapBatch{}, err
	}
	matching, _ := result.All.(imap.UIDSet)
	for _, match := range matching {
		if !uidRange.Contains(match.Start) || !uidRange.Contains(match.Stop) {
			return imapBatch{}, errors.New("IMAP search returned a UID outside the requested range")
		}
	}
	uids := make([]imap.UID, 0, batchSize)
	for candidate := last + 1; candidate <= end; candidate++ {
		if matching.Contains(imap.UID(candidate)) {
			if len(uids) == batchSize {
				end = candidate - 1
				break
			}
			uids = append(uids, imap.UID(candidate))
		}
	}
	return imapBatch{uids: uids, validity: validity, last: end, more: end < highest}, nil
}

func (c *imapConnection) fetch(uid imap.UID, limit int) (*message.Entity, error) {
	section := &imap.FetchItemBodySection{Peek: true, Partial: &imap.SectionPartial{Size: int64(limit) + 1}}
	command := c.client.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{
		UID: true, RFC822Size: true, BodySection: []*imap.FetchItemBodySection{section},
	})
	defer command.Close()
	fetched := command.Next()
	if fetched == nil {
		// A message can be expunged between SEARCH and FETCH.
		return nil, command.Close()
	}
	var receivedUID imap.UID
	var data []byte
	var size int64 = -1
	for item := fetched.Next(); item != nil; item = fetched.Next() {
		switch item := item.(type) {
		case imapclient.FetchItemDataUID:
			receivedUID = item.UID
		case imapclient.FetchItemDataRFC822Size:
			size = item.Size
		case imapclient.FetchItemDataBodySection:
			if item.Literal != nil {
				var err error
				data, err = readLimited(item.Literal, limit)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	if err := command.Close(); err != nil {
		return nil, err
	}
	if receivedUID != uid || size != int64(len(data)) || data == nil {
		return nil, errors.New("IMAP fetch did not return the complete requested message")
	}
	return message.Read(bytes.NewReader(data))
}

func imapProviderUID(config ReceiveInput, validity int64, uid imap.UID) string {
	source := fmt.Sprintf("%s\x00%d\x00%s", strings.ToLower(config.Host), config.Port, config.Username)
	return fmt.Sprintf("imap:%x:%d:%d", sha256.Sum256([]byte(source)), validity, uid)
}
