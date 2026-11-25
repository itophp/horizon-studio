ALTER TABLE mail_channels
    DROP CONSTRAINT mail_channels_check,
    DROP CONSTRAINT mail_channels_protocol_check,
    ADD COLUMN imap_uid_validity bigint DEFAULT 0 NOT NULL,
    ADD COLUMN imap_last_uid bigint DEFAULT 0 NOT NULL,
    ADD CONSTRAINT mail_channels_imap_uid_validity_check CHECK (imap_uid_validity BETWEEN 0 AND 4294967295),
    ADD CONSTRAINT mail_channels_imap_last_uid_check CHECK (imap_last_uid BETWEEN 0 AND 4294967295),
    ADD CONSTRAINT mail_channels_protocol_check CHECK (protocol IN ('smtp', 'pop3', 'imap')),
    ADD CONSTRAINT mail_channels_check CHECK (
        (direction = 'send' AND protocol = 'smtp') OR
        (direction = 'receive' AND (protocol = 'imap' OR (protocol = 'pop3' AND tls_mode = 'tls')))
    );
