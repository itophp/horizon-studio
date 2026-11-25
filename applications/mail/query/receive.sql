-- name: ReceiveDue :one
SELECT m.id AS mailbox_id, m.tenant_id, m.owner_account_id, c.id AS channel_id,
       c.protocol, c.host, c.port, c.tls_mode, c.username, c.imap_uid_validity, c.imap_last_uid,
       cr.ciphertext, cr.version
FROM mail_channels c JOIN mail_mailboxes m ON m.tenant_id=c.tenant_id AND m.id=c.mailbox_id
JOIN iam_accounts a ON a.tenant_id=m.tenant_id AND a.id=m.owner_account_id
JOIN mail_credentials cr ON cr.tenant_id=c.tenant_id AND cr.id=c.credential_id
WHERE c.tenant_id=$1 AND c.id=$2 AND c.protocol=$3 AND c.direction='receive'
  AND m.status='active' AND (c.next_sync_at IS NULL OR c.next_sync_at<=now())
  AND (c.lease_until IS NULL OR c.lease_until<now())
FOR UPDATE OF c;

-- name: ReceiveDueIDs :many
SELECT c.id, c.protocol FROM mail_channels c JOIN mail_mailboxes m ON m.tenant_id=c.tenant_id AND m.id=c.mailbox_id
WHERE c.tenant_id=$1 AND c.direction='receive' AND c.protocol IN ('pop3', 'imap') AND m.status='active'
  AND (c.next_sync_at IS NULL OR c.next_sync_at<=now())
  AND (c.lease_until IS NULL OR c.lease_until<now())
ORDER BY c.next_sync_at NULLS FIRST,c.id LIMIT 20;

-- name: PendingPOP3UIDs :many
SELECT pending.uid::text
FROM unnest(sqlc.arg(uids)::text[]) WITH ORDINALITY AS pending(uid, position)
WHERE NOT EXISTS (
  SELECT 1 FROM mail_messages m
  WHERE m.tenant_id=sqlc.arg(tenant_id) AND m.mailbox_id=sqlc.arg(mailbox_id)
    AND m.direction='inbound' AND m.provider_uid=pending.uid
)
ORDER BY pending.position LIMIT sqlc.arg(max_rows);

-- name: ReceiveSettingsUpdate :exec
UPDATE mail_channels c SET protocol=sqlc.arg(protocol), host=sqlc.arg(host), port=sqlc.arg(port),
  tls_mode=sqlc.arg(tls_mode), username=sqlc.arg(username), verification_status='unknown',
  last_error_code='', last_verified_at=NULL, updated_at=now(), next_sync_at=NULL,
  imap_uid_validity=0, imap_last_uid=0
FROM mail_mailboxes m
WHERE c.tenant_id=m.tenant_id AND c.mailbox_id=m.id AND m.owner_account_id=sqlc.arg(owner_account_id)
  AND c.tenant_id=sqlc.arg(tenant_id) AND c.mailbox_id=sqlc.arg(mailbox_id) AND c.direction='receive'
  AND (c.protocol, c.host, c.port, c.tls_mode, c.username) IS DISTINCT FROM
      (sqlc.arg(protocol), sqlc.arg(host), sqlc.arg(port), sqlc.arg(tls_mode), sqlc.arg(username));

-- name: IMAPCursorSave :exec
UPDATE mail_channels SET imap_uid_validity=$1, imap_last_uid=$2
WHERE tenant_id=$3 AND id=$4 AND direction='receive' AND protocol='imap';
