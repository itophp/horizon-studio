package audit

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"horizon/internal/repository/dbgen"
)

const checkpointBatchSize = 512

var ErrAuditIntegrity = errors.New("security audit checkpoint integrity check failed")
var ErrAuditExpired = errors.New("security audit checkpoint records expired under the retention policy")

type correlationKey struct{}

func WithCorrelation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

func Correlation(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return id
}

type SecurityLog struct {
	pool       *pgxpool.Pool
	privateKey ed25519.PrivateKey
	keyID      string
	publicKeys map[string]ed25519.PublicKey
}

func NewSecurityLog(pool *pgxpool.Pool, seed []byte, previous ...ed25519.PublicKey) *SecurityLog {
	log := &SecurityLog{pool: pool, publicKeys: map[string]ed25519.PublicKey{}}
	for _, key := range previous {
		if len(key) == ed25519.PublicKeySize {
			log.publicKeys[auditKeyID(key)] = bytes.Clone(key)
		}
	}
	if len(seed) == ed25519.SeedSize {
		log.privateKey = ed25519.NewKeyFromSeed(seed)
		public := log.privateKey.Public().(ed25519.PublicKey)
		log.keyID = auditKeyID(public)
		log.publicKeys[log.keyID] = public
	}
	return log
}

func auditKeyID(key ed25519.PublicKey) string {
	digest := sha256.Sum256(key)
	return hex.EncodeToString(digest[:])
}

func (s *SecurityLog) Configured() bool {
	return s != nil && s.pool != nil && len(s.privateKey) == ed25519.PrivateKeySize
}

func (s *SecurityLog) Run(ctx context.Context, onlineDays, retentionDays int) {
	if !s.Configured() {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := s.Checkpoint(ctx); err != nil && ctx.Err() == nil {
			slog.Error("security audit checkpoint failed", "integrity", errors.Is(err, ErrAuditIntegrity))
		} else if ctx.Err() == nil {
			now := time.Now().UTC()
			if onlineDays > 0 {
				if _, err := s.ArchiveBefore(ctx, now.AddDate(0, 0, -onlineDays)); err != nil {
					slog.Error("security audit archive failed", "integrity", errors.Is(err, ErrAuditIntegrity))
				}
			}
			if retentionDays > 0 {
				if err := s.ExpireBefore(ctx, now.AddDate(0, 0, -retentionDays)); err != nil {
					slog.Error("security audit retention failed", "integrity", errors.Is(err, ErrAuditIntegrity))
				}
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

// ArchiveBefore moves at most one verified checkpoint without changing its
// signed content or its visibility through the authorized history query.
func (s *SecurityLog) ArchiveBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if !s.Configured() {
		return 0, ErrAuditIntegrity
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := dbgen.New(tx)
	if err = q.SecurityAuditLock(ctx); err != nil {
		return 0, err
	}
	id, err := q.SecurityAuditArchivableCheckpoint(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	checkpoint, err := q.SecurityAuditCheckpoint(ctx, id)
	if err != nil {
		return 0, err
	}
	if err = s.verifyCheckpoint(ctx, q, checkpoint); err != nil {
		return 0, err
	}
	if err = q.SecurityAuditCopyCheckpointToArchive(ctx, dbgen.SecurityAuditCopyCheckpointToArchiveParams{
		FirstID: checkpoint.PreviousID, LastID: checkpoint.LastID,
	}); err != nil {
		return 0, err
	}
	moved, err := q.SecurityAuditMoveArchivedCheckpoint(ctx, dbgen.SecurityAuditMoveArchivedCheckpointParams{
		FirstID: checkpoint.PreviousID, LastID: checkpoint.LastID,
	})
	if err != nil {
		return 0, err
	}
	return moved, tx.Commit(ctx)
}

func (s *SecurityLog) Checkpoint(ctx context.Context) (int64, error) {
	if !s.Configured() {
		return 0, ErrAuditIntegrity
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	queries := dbgen.New(tx)
	if err := queries.SecurityAuditLock(ctx); err != nil {
		return 0, err
	}
	last, err := queries.SecurityAuditLastCheckpoint(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		last.Digest = make([]byte, sha256.Size)
	} else if err != nil {
		return 0, err
	} else if err = s.verifyCheckpoint(ctx, queries, last); err != nil && !errors.Is(err, ErrAuditExpired) {
		return 0, err
	}
	rows, err := queries.SecurityAuditBatch(ctx, dbgen.SecurityAuditBatchParams{
		FirstID: last.LastID, LastID: math.MaxInt64, MaxRows: checkpointBatchSize,
	})
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return last.LastID, tx.Commit(ctx)
	}
	digest, err := checkpointDigest(last.Digest, rows)
	if err != nil {
		return 0, err
	}
	lastID := rows[len(rows)-1].ID
	if err = queries.SecurityAuditSaveCheckpoint(ctx, dbgen.SecurityAuditSaveCheckpointParams{
		LastID: lastID, PreviousID: last.LastID, Digest: digest, PreviousDigest: last.Digest,
		Signature: ed25519.Sign(s.privateKey, checkpointMessage(last.LastID, lastID, digest, last.Digest)),
		KeyID:     s.keyID,
	}); err != nil {
		return 0, err
	}
	return lastID, tx.Commit(ctx)
}

func (s *SecurityLog) Verify(ctx context.Context, checkpointID int64) error {
	if s == nil || s.pool == nil || len(s.publicKeys) == 0 {
		return ErrAuditIntegrity
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	queries := dbgen.New(tx)
	checkpoint, err := queries.SecurityAuditCheckpoint(ctx, checkpointID)
	if err != nil {
		return err
	}
	return s.verifyCheckpoint(ctx, queries, checkpoint)
}

func (s *SecurityLog) verifyCheckpoint(ctx context.Context, queries *dbgen.Queries, checkpoint dbgen.IamSecurityCheckpoint) error {
	if !s.verifySignature(checkpoint) {
		return ErrAuditIntegrity
	}
	if checkpoint.PreviousID > 0 {
		previous, err := queries.SecurityAuditCheckpoint(ctx, checkpoint.PreviousID)
		if err != nil || !bytes.Equal(previous.Digest, checkpoint.PreviousDigest) ||
			!s.verifySignature(previous) {
			return ErrAuditIntegrity
		}
	}
	rows, err := queries.SecurityAuditBatch(ctx, dbgen.SecurityAuditBatchParams{
		FirstID: checkpoint.PreviousID, LastID: checkpoint.LastID, MaxRows: checkpointBatchSize + 1,
	})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		receipt, err := queries.SecurityAuditRetentionReceipt(ctx, checkpoint.LastID)
		if err == nil && s.verifyRetentionReceipt(checkpoint, receipt) {
			return ErrAuditExpired
		}
		return ErrAuditIntegrity
	}
	if len(rows) > checkpointBatchSize || rows[len(rows)-1].ID != checkpoint.LastID {
		return ErrAuditIntegrity
	}
	digest, err := checkpointDigest(checkpoint.PreviousDigest, rows)
	if err != nil || !bytes.Equal(digest, checkpoint.Digest) {
		return ErrAuditIntegrity
	}
	return nil
}

func (s *SecurityLog) verifySignature(checkpoint dbgen.IamSecurityCheckpoint) bool {
	message := checkpointMessage(checkpoint.PreviousID, checkpoint.LastID, checkpoint.Digest, checkpoint.PreviousDigest)
	if checkpoint.KeyID != "" {
		public := s.publicKeys[checkpoint.KeyID]
		return len(public) == ed25519.PublicKeySize && ed25519.Verify(public, message, checkpoint.Signature)
	}
	for _, public := range s.publicKeys {
		if ed25519.Verify(public, message, checkpoint.Signature) {
			return true
		}
	}
	return false
}

func checkpointDigest(previous []byte, rows []dbgen.IamSecurityEventHistory) ([]byte, error) {
	hash := sha256.New()
	_, _ = hash.Write(previous)
	encoder := json.NewEncoder(hash)
	for _, row := range rows {
		row.CreatedAt.Time = row.CreatedAt.Time.UTC()
		// Preserve the original signed field order for records predating authority metadata.
		record := struct {
			ID            int64
			TenantID      string
			AccountID     int64
			DepartmentID  int64 // Fixed at zero to retain the signed checkpoint format.
			BindingID     pgtype.UUID
			Action        string
			Outcome       string
			Reason        string
			CorrelationID string
			ResourceType  string
			ResourceID    string
			CreatedAt     pgtype.Timestamptz
			Authority     json.RawMessage `json:",omitempty"`
		}{
			ID: row.ID, TenantID: row.TenantID, AccountID: row.AccountID,
			BindingID: row.BindingID, Action: row.Action, Outcome: row.Outcome, Reason: row.Reason, CorrelationID: row.CorrelationID,
			ResourceType: row.ResourceType, ResourceID: row.ResourceID, CreatedAt: row.CreatedAt, Authority: row.Authority,
		}
		if err := encoder.Encode(record); err != nil {
			return nil, err
		}
	}
	return hash.Sum(nil), nil
}

func checkpointMessage(previousID, lastID int64, digest, previous []byte) []byte {
	message := append([]byte("horizon-iam-audit/v1\x00"), previous...)
	message = binary.BigEndian.AppendUint64(message, uint64(previousID))
	message = binary.BigEndian.AppendUint64(message, uint64(lastID))
	return append(message, digest...)
}
