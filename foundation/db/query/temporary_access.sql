-- name: IAMCreateTemporaryRequest :one
INSERT INTO iam_temporary_requests(tenant_id,applicant_id,requested_grants,requested_minutes,reason) VALUES($1,$2,$3,$4,$5) RETURNING id;

-- name: IAMListTemporaryRequests :many
SELECT r.id,r.applicant_id,a.username AS applicant_name,r.requested_grants,r.requested_minutes,r.reason,
 CASE WHEN r.status='approved' AND r.expires_at<=now() THEN 'expired' ELSE r.status END::text AS status,
 r.approved_grants,r.approved_minutes,COALESCE(r.reviewer_id,0)::bigint AS reviewer_id,COALESCE(reviewer.username,'')::text AS reviewer_name,r.review_note,r.created_at,r.reviewed_at,r.expires_at
FROM iam_temporary_requests r JOIN iam_accounts a ON a.tenant_id=r.tenant_id AND a.id=r.applicant_id
LEFT JOIN iam_accounts reviewer ON reviewer.tenant_id=r.tenant_id AND reviewer.id=r.reviewer_id
WHERE r.tenant_id=sqlc.arg(tenant_id) AND (sqlc.arg(all_requests)::boolean OR r.applicant_id=sqlc.arg(applicant_id)) ORDER BY r.created_at DESC,r.id DESC;

-- name: IAMGetTemporaryRequest :one
SELECT id,applicant_id,status FROM iam_temporary_requests WHERE tenant_id=$1 AND id=$2;

-- name: IAMReviewTemporaryRequest :one
UPDATE iam_temporary_requests SET status=sqlc.arg(status),approved_grants=sqlc.arg(approved_grants),resolved_scopes=sqlc.arg(resolved_scopes),approved_minutes=sqlc.arg(approved_minutes),reviewer_id=sqlc.arg(reviewer_id),review_note=sqlc.arg(review_note),reviewed_at=now(),
 expires_at=CASE WHEN sqlc.arg(status)::text='approved' THEN now()+make_interval(mins=>sqlc.arg(approved_minutes)::int) ELSE NULL END
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND status='pending' AND applicant_id<>sqlc.arg(reviewer_id)
RETURNING id;

-- name: IAMCancelTemporaryRequest :one
UPDATE iam_temporary_requests SET status='cancelled',cancelled_at=now() WHERE tenant_id=$1 AND id=$2 AND applicant_id=$3 AND status='pending' RETURNING id;

-- name: IAMRevokeTemporaryRequest :one
UPDATE iam_temporary_requests SET status='revoked',revoked_at=now(),revoked_by=sqlc.arg(applicant_id) WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND (sqlc.arg(can_review)::boolean OR applicant_id=sqlc.arg(applicant_id)) AND status='approved' AND expires_at>now() RETURNING id;

-- name: IAMActiveTemporaryGrants :many
SELECT r.approved_grants,r.resolved_scopes
FROM iam_temporary_requests r
JOIN iam_active_account_subjects reviewer ON reviewer.tenant_id=r.tenant_id AND reviewer.account_id=r.reviewer_id
WHERE r.tenant_id=$1 AND r.applicant_id=$2 AND r.status='approved' AND r.expires_at>now();
