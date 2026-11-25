package audit

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/internal/repository/dbgen"
)

// ExpireBefore removes one complete archived batch after signing its receipt in
// the same transaction. The checkpoint digest remains in the original chain.
func (s *SecurityLog) ExpireBefore(ctx context.Context, cutoff time.Time) error {
	if !s.Configured() || cutoff.After(time.Now()) {
		return ErrAuditIntegrity
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := dbgen.New(tx)
	if err = q.SecurityAuditLock(ctx); err != nil {
		return err
	}
	cutoff = cutoff.UTC().Truncate(time.Microsecond)
	at := pgtype.Timestamptz{Time: cutoff, Valid: true}
	checkpoint, err := q.SecurityAuditExpiredArchiveCheckpoint(ctx, at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = s.verifyCheckpoint(ctx, q, checkpoint); err != nil {
		return err
	}
	if err = q.SecurityAuditSaveRetentionReceipt(ctx, dbgen.SecurityAuditSaveRetentionReceiptParams{
		CheckpointID: checkpoint.LastID, Cutoff: at, KeyID: s.keyID,
		Signature: ed25519.Sign(s.privateKey, retentionReceiptMessage(checkpoint, cutoff)),
	}); err != nil {
		return err
	}
	if err = q.SecurityAuditDeleteExpiredArchive(ctx, dbgen.SecurityAuditDeleteExpiredArchiveParams{
		FirstID: checkpoint.PreviousID, LastID: checkpoint.LastID,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *SecurityLog) verifyRetentionReceipt(checkpoint dbgen.IamSecurityCheckpoint, receipt dbgen.IamSecurityRetentionReceipt) bool {
	public := s.publicKeys[receipt.KeyID]
	return receipt.Cutoff.Valid && receipt.Cutoff.Time.After(checkpoint.CreatedAt.Time) &&
		len(public) == ed25519.PublicKeySize && ed25519.Verify(public, retentionReceiptMessage(checkpoint, receipt.Cutoff.Time), receipt.Signature)
}

func retentionReceiptMessage(checkpoint dbgen.IamSecurityCheckpoint, cutoff time.Time) []byte {
	message := append([]byte("horizon-iam-retention/v1\x00"), checkpointMessage(checkpoint.PreviousID, checkpoint.LastID, checkpoint.Digest, checkpoint.PreviousDigest)...)
	return binary.BigEndian.AppendUint64(message, uint64(cutoff.UnixMicro()))
}
