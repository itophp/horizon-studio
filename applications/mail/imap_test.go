package mail

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2/imapclient"
	"horizon/foundation/system/governance"
)

func TestIMAPReadOnlyUIDBatch(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	body := "Subject: IMAP message\r\n\r\nBody\r\n"
	serverResult := make(chan error, 1)
	go func() {
		defer server.Close()
		_, err := fmt.Fprint(server, "* OK [CAPABILITY IMAP4rev1] ready\r\n")
		if err != nil {
			serverResult <- err
			return
		}
		commands := []struct{ want, response string }{
			{"EXAMINE INBOX", "* 3 EXISTS\r\n* OK [UIDVALIDITY 11] valid\r\n* OK [UIDNEXT 9] next\r\n"},
			{"UID SEARCH UID 1:8 SMALLER 1025", "* SEARCH 8 4 2\r\n"},
			{"UID FETCH 2 (UID RFC822.SIZE BODY.PEEK[]<0.1025>)", fmt.Sprintf("* 1 FETCH (UID 2 RFC822.SIZE %d BODY[]<0> {%d}\r\n%s)\r\n", len(body), len(body), body)},
		}
		reader := bufio.NewReader(server)
		for _, step := range commands {
			line, err := reader.ReadString('\n')
			if err != nil {
				serverResult <- err
				return
			}
			tag, command, _ := strings.Cut(strings.TrimSpace(line), " ")
			if command != step.want {
				serverResult <- fmt.Errorf("IMAP command %q, want %q", command, step.want)
				return
			}
			if _, err := fmt.Fprintf(server, "%s%s OK completed\r\n", step.response, tag); err != nil {
				serverResult <- err
				return
			}
		}
		serverResult <- nil
	}()
	connection := &imapConnection{client: imapclient.New(client, nil)}
	batch, err := connection.batch(10, 40, 2, 1024)
	if err != nil {
		t.Fatalf("batch error: %v (server: %v)", err, <-serverResult)
	}
	if len(batch.uids) != 2 || batch.uids[0] != 2 || batch.uids[1] != 4 || batch.last != 7 || batch.validity != 11 || !batch.more {
		t.Fatalf("unexpected IMAP UID batch: %+v", batch)
	}
	entity, err := connection.fetch(batch.uids[0], 1024)
	if serverErr := <-serverResult; serverErr != nil {
		t.Fatal(serverErr)
	}
	if err != nil || entity == nil || entity.Header.Get("Subject") != "IMAP message" {
		t.Fatalf("fetch entity = %v, error = %v", entity, err)
	}
}

func TestIMAPRequiresAllowedTLSConnection(t *testing.T) {
	config := ReceiveInput{Host: "imap.example.test", Port: 993, Username: "owner", TLSMode: "tls"}
	if _, err := imapConnect(t.Context(), config, "app-password", []string{"imap.example.test:143"}); governance.Code(err) != "forbidden" {
		t.Fatalf("expected exact endpoint allowlist denial, got %v", err)
	}
	config.TLSMode = ""
	if _, err := imapConnect(t.Context(), config, "app-password", []string{"imap.example.test:993"}); governance.Code(err) != "invalid" {
		t.Fatalf("expected missing TLS mode denial, got %v", err)
	}
}
