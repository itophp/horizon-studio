-- name: IAMOpenExternalReview :one
INSERT INTO iam_external_identity_reviews(tenant_id, external_identity_id, original_login_id, requested_login_id, original_linked_at)
SELECT sqlc.arg(tenant_id), external.id, external.login_account_id, sqlc.arg(requested_login_id), external.linked_at
FROM iam_external_identities external
JOIN iam_active_account_subjects original ON original.tenant_id = sqlc.arg(tenant_id)
     AND original.login_account_id = external.login_account_id
JOIN iam_password_credentials credential ON credential.login_account_id = external.login_account_id
WHERE external.provider_id = sqlc.arg(provider_id) AND external.issuer = sqlc.arg(issuer)
    AND external.subject = sqlc.arg(subject) AND external.revoked_at IS NULL
    AND external.login_account_id <> sqlc.arg(requested_login_id)
    AND NOT EXISTS (
        SELECT 1 FROM iam_external_identity_reviews review
        WHERE review.tenant_id = sqlc.arg(tenant_id) AND review.external_identity_id = external.id
          AND review.requested_login_id = sqlc.arg(requested_login_id) AND review.status IN ('pending','confirmed')
          AND review.expires_at > statement_timestamp()
    )
RETURNING id;

-- name: IAMListExternalReviews :many
SELECT review.id, CASE
           WHEN review.status IN ('pending', 'confirmed') AND review.expires_at <= statement_timestamp()
               THEN 'expired'
           ELSE review.status
       END::text AS status, external.provider_id, review.created_at, review.expires_at,
       review.applied_at, COALESCE(review.reason, '')::text AS reason,
       review.original_login_id = sqlc.arg(login_id)::uuid AS original_owner,
       review.requested_login_id = sqlc.arg(login_id)::uuid AS requested_owner
FROM iam_external_identity_reviews review
JOIN iam_external_identities external ON external.id = review.external_identity_id
WHERE review.tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.arg(reviewer)::boolean OR review.original_login_id = sqlc.arg(login_id) OR review.requested_login_id = sqlc.arg(login_id))
  AND (sqlc.narg(after_id)::uuid IS NULL OR review.id > sqlc.narg(after_id)::uuid)
ORDER BY review.id LIMIT 51;

-- name: IAMConfirmExternalReview :one
UPDATE iam_external_identity_reviews review
SET status = 'confirmed'
WHERE review.tenant_id = sqlc.arg(tenant_id) AND review.id = sqlc.arg(id)
  AND review.status = 'pending' AND review.expires_at > statement_timestamp()
  AND review.original_login_id = sqlc.arg(login_id)
  AND EXISTS (
      SELECT 1 FROM iam_sessions session JOIN iam_external_identities external ON external.id = session.external_identity_id
      WHERE session.token_hash = sqlc.arg(session_hash) AND session.csrf_token = sqlc.arg(csrf_token)
        AND session.created_at > statement_timestamp() - interval '5 minutes' AND session.expires_at > statement_timestamp()
        AND external.id = review.external_identity_id AND external.linked_at = review.original_linked_at AND external.login_account_id = review.original_login_id AND external.revoked_at IS NULL
  )
RETURNING review.id;

-- name: IAMLockExternalReview :one
SELECT review.id, review.external_identity_id, review.original_login_id, review.requested_login_id, review.status, review.applied_at, review.original_linked_at
FROM iam_external_identity_reviews review
WHERE review.tenant_id = sqlc.arg(tenant_id) AND review.id = sqlc.arg(id)
  AND review.original_login_id <> sqlc.arg(reviewer_login_id) AND review.requested_login_id <> sqlc.arg(reviewer_login_id)
  AND (review.expires_at > statement_timestamp() OR review.status = 'applied')
FOR UPDATE OF review;

-- name: IAMTransferExternalIdentity :one
UPDATE iam_external_identities external
SET login_account_id = sqlc.arg(next_login_id), linked_at = statement_timestamp()
WHERE external.id = sqlc.arg(external_id) AND external.login_account_id = sqlc.arg(previous_login_id)
  AND external.revoked_at IS NULL
  AND external.linked_at = sqlc.arg(expected_linked_at)::timestamptz
  AND EXISTS (SELECT 1 FROM iam_password_credentials credential WHERE credential.login_account_id = external.login_account_id)
  AND EXISTS (SELECT 1 FROM iam_password_credentials credential WHERE credential.login_account_id = sqlc.arg(next_login_id))
  AND EXISTS (
      SELECT 1 FROM iam_active_account_subjects subject
      WHERE subject.tenant_id = sqlc.arg(tenant_id) AND subject.login_account_id = sqlc.arg(next_login_id)
  )
RETURNING external.linked_at;

-- name: IAMDecideExternalReview :one
UPDATE iam_external_identity_reviews review
SET status = sqlc.arg(status), reason = sqlc.arg(reason)::text,
    decided_by = sqlc.arg(reviewer_login_id), applied_at = COALESCE(sqlc.narg(applied_at)::timestamptz, applied_at)
WHERE review.tenant_id = sqlc.arg(tenant_id) AND review.id = sqlc.arg(id)
RETURNING review.id;
