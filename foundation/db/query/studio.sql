-- HorizonStudio queries. A managed project is visible to the account that
-- linked or created it (owner = created_by); requirements, items and runs are
-- reached only through that owned project row, so a caller who knows a foreign
-- id still reads nothing. Every statement filters tenant_id first and projects
-- the minimal column set.

-- name: StudioProjectList :many
SELECT p.id, p.root, p.name, p.development_brief, p.created_at, p.updated_at,
       p.ai_analysis, p.ai_analyzed_at, counts.requirements_total, counts.requirements_pending
FROM studio_projects p
CROSS JOIN LATERAL (
  SELECT count(*)::bigint AS requirements_total,
         count(*) FILTER (WHERE NOT r.archived AND (
           NOT EXISTS (
             SELECT 1 FROM studio_items i
             WHERE i.tenant_id = p.tenant_id AND i.project_id = p.id
               AND i.requirement_id = r.id AND i.kind = 'execution' AND NOT i.archived AND i.stage = 'accepted'
           )
           OR EXISTS (
             SELECT 1 FROM studio_items i
             WHERE i.tenant_id = p.tenant_id AND i.project_id = p.id
               AND i.requirement_id = r.id AND i.kind = 'execution' AND NOT i.archived AND i.stage <> 'accepted'
           )
         ))::bigint AS requirements_pending
  FROM studio_requirements r
  WHERE r.tenant_id = p.tenant_id AND r.project_id = p.id
) counts
WHERE p.tenant_id = $1 AND p.created_by = $2
ORDER BY p.id DESC;

-- name: StudioProjectGet :one
SELECT p.id, p.root, p.name, p.development_brief, p.created_at, p.updated_at, p.ai_analysis, p.ai_analyzed_at
FROM studio_projects p
WHERE p.tenant_id = $1 AND p.id = $2 AND p.created_by = $3;

-- name: StudioProjectCreate :one
INSERT INTO studio_projects (tenant_id, root, created_by, name, development_brief)
VALUES ($1, $2, $3, $4, COALESCE(sqlc.narg(development_brief)::text, ''))
ON CONFLICT (tenant_id, root) DO UPDATE
SET name = COALESCE(NULLIF(EXCLUDED.name, ''), studio_projects.name),
    development_brief = COALESCE(sqlc.narg(development_brief)::text, studio_projects.development_brief),
    updated_at = now()
WHERE studio_projects.created_by = EXCLUDED.created_by
RETURNING id, root, name, development_brief, created_at, updated_at;

-- name: StudioProjectSaveAnalysis :one
UPDATE studio_projects
SET ai_analysis = $4, ai_analyzed_at = now()
WHERE tenant_id = $1 AND id = $2 AND created_by = $3
RETURNING ai_analyzed_at;

-- Requirements. Archive keeps the record readable; the join guarantees the
-- caller owns the parent project.

-- name: StudioRequirementList :many
SELECT r.id, r.project_id, r.parent_id, r.title, r.description, r.source, r.archived, r.created_at, r.updated_at
FROM studio_requirements r
JOIN studio_projects p ON p.tenant_id = r.tenant_id AND p.id = r.project_id
WHERE r.tenant_id = $1 AND r.project_id = $2 AND p.created_by = $3
ORDER BY r.id DESC;

-- name: StudioRequirementGet :one
SELECT r.id, r.project_id, r.parent_id, r.title, r.description, r.source, r.archived, r.created_at, r.updated_at
FROM studio_requirements r
JOIN studio_projects p ON p.tenant_id = r.tenant_id AND p.id = r.project_id
WHERE r.tenant_id = $1 AND r.id = $2 AND p.created_by = $3;

-- name: StudioRequirementChildCount :one
SELECT count(*)::bigint FROM studio_requirements
WHERE tenant_id = $1 AND parent_id = sqlc.arg(parent_id)::bigint;

-- Edit one requirement and optionally move it in the two-level tree. An
-- archived requirement stays read-only, and the join guarantees the caller
-- owns the parent project. Omitted description/source keep their stored text;
-- an explicit empty string clears them. An omitted parent keeps the current
-- node; 0 promotes it to a top-level requirement; another id moves it under
-- that top-level requirement.
-- name: StudioRequirementUpdate :one
UPDATE studio_requirements r
SET title = sqlc.arg(title),
    description = COALESCE(sqlc.narg(description)::text, r.description),
    source = COALESCE(sqlc.narg(source)::text, r.source),
    parent_id = CASE
      WHEN sqlc.narg(parent_id)::bigint IS NULL THEN r.parent_id
      ELSE NULLIF(sqlc.narg(parent_id)::bigint, 0)
    END,
    updated_at = now()
FROM studio_projects p
WHERE r.tenant_id = sqlc.arg(tenant_id)
  AND r.id = sqlc.arg(id)
  AND r.project_id = sqlc.arg(project_id)
  AND r.project_id = p.id AND p.tenant_id = r.tenant_id
  AND p.created_by = sqlc.arg(created_by)
  AND r.archived = false
  AND (sqlc.narg(parent_id)::bigint IS NULL OR sqlc.narg(parent_id)::bigint = 0 OR EXISTS (
    SELECT 1 FROM studio_requirements parent
    WHERE parent.tenant_id = r.tenant_id AND parent.project_id = r.project_id
      AND parent.id = sqlc.narg(parent_id) AND parent.parent_id IS NULL AND NOT parent.archived
  ))
  AND (sqlc.narg(parent_id)::bigint IS NULL OR sqlc.narg(parent_id)::bigint = 0 OR NOT EXISTS (
    SELECT 1 FROM studio_requirements child
    WHERE child.tenant_id = r.tenant_id AND child.parent_id = r.id
  ))
RETURNING r.id, r.title, r.archived, r.parent_id;

-- name: StudioRequirementCreate :one
INSERT INTO studio_requirements (tenant_id, project_id, parent_id, title, description, source, created_by)
SELECT $1, p.id, sqlc.narg(parent_id)::bigint, $3, $4, $5, $6
FROM studio_projects p
WHERE p.tenant_id = $1 AND p.id = $2 AND p.created_by = $6
  AND (sqlc.narg(parent_id)::bigint IS NULL OR EXISTS (
    SELECT 1 FROM studio_requirements parent
    WHERE parent.tenant_id = p.tenant_id AND parent.project_id = p.id
      AND parent.id = sqlc.narg(parent_id) AND parent.parent_id IS NULL AND NOT parent.archived
  ))
RETURNING id, project_id, parent_id, title, description, source, archived, created_at, updated_at;

-- Archive one requirement and every active sub-requirement in the same
-- transaction. Only rows the caller owns through the project are touched.
-- name: StudioRequirementArchiveTree :many
WITH target AS (
  SELECT r.id, r.tenant_id, r.project_id
  FROM studio_requirements r
  JOIN studio_projects p ON p.tenant_id = r.tenant_id AND p.id = r.project_id
  WHERE r.tenant_id = $1 AND r.id = $2 AND p.created_by = $3 AND r.archived = false
), archived AS (
  UPDATE studio_requirements r
  SET archived = true, updated_at = now()
  FROM target t
  WHERE r.tenant_id = t.tenant_id AND r.project_id = t.project_id
    AND (r.id = t.id OR r.parent_id = t.id)
    AND r.archived = false
  RETURNING r.id
)
SELECT id FROM archived ORDER BY id;

-- Search top-level requirements for a parent selector. Children and archived
-- nodes are not valid parents, so they never appear as candidates.
-- name: StudioRequirementSearch :many
SELECT r.id, r.title
FROM studio_requirements r
JOIN studio_projects p ON p.tenant_id = r.tenant_id AND p.id = r.project_id
WHERE r.tenant_id = $1 AND r.project_id = $2 AND p.created_by = $3
  AND r.parent_id IS NULL AND NOT r.archived
  AND (sqlc.arg(keyword)::text = '' OR strpos(lower(r.title), lower(sqlc.arg(keyword)::text)) > 0)
  AND (sqlc.arg(exclude)::bigint = 0 OR r.id <> sqlc.arg(exclude)::bigint)
ORDER BY r.id DESC
LIMIT 20;

-- Requirement attachments. The binding owns no bytes; the shared file service
-- stores them and the caller must own the uploaded file to bind it.

-- name: StudioRequirementAttachmentList :many
SELECT f.id, f.name, f.mime, f.size, b.created_at
FROM studio_requirement_files b
JOIN studio_requirements r ON r.tenant_id = b.tenant_id AND r.id = b.requirement_id
JOIN studio_projects p ON p.tenant_id = r.tenant_id AND p.id = r.project_id
JOIN gov_files f ON f.tenant_id = b.tenant_id AND f.id = b.file_id
WHERE b.tenant_id = $1 AND b.requirement_id = $2 AND r.project_id = $3 AND p.created_by = $4
  AND f.deleted_at IS NULL
ORDER BY b.created_at DESC, f.id DESC;

-- name: StudioRequirementAttachmentAdd :one
INSERT INTO studio_requirement_files (tenant_id, requirement_id, file_id, created_by)
SELECT sqlc.arg(tenant_id), r.id, f.id, sqlc.arg(created_by)
FROM studio_requirements r
JOIN studio_projects p ON p.tenant_id = r.tenant_id AND p.id = r.project_id
JOIN gov_files f ON f.tenant_id = sqlc.arg(tenant_id) AND f.id = sqlc.arg(file_id)
WHERE r.tenant_id = sqlc.arg(tenant_id) AND r.id = sqlc.arg(requirement_id)
  AND r.project_id = sqlc.arg(project_id) AND p.created_by = sqlc.arg(created_by)
  AND NOT r.archived
  AND f.owner_id = sqlc.arg(created_by) AND f.purpose = 'attachment' AND f.deleted_at IS NULL
ON CONFLICT (tenant_id, requirement_id, file_id) DO UPDATE SET created_by = EXCLUDED.created_by
RETURNING id, file_id, created_at;

-- name: StudioRequirementAttachmentRemove :one
DELETE FROM studio_requirement_files b
USING studio_requirements r, studio_projects p
WHERE b.tenant_id = $1 AND b.requirement_id = $2 AND b.file_id = $3
  AND r.tenant_id = b.tenant_id AND r.id = b.requirement_id
  AND p.tenant_id = r.tenant_id AND p.id = r.project_id AND p.created_by = $4
  AND NOT r.archived
RETURNING b.file_id;

-- name: StudioOwnedAttachment :one
SELECT id, name, mime, size FROM gov_files
WHERE tenant_id = $1 AND id = $2 AND owner_id = $3
  AND purpose = 'attachment' AND deleted_at IS NULL;

-- Work items.

-- name: StudioItemList :many
SELECT i.id, i.project_id, i.requirement_id, i.kind, i.title, i.stage,
       i.reference, i.reference_kind, i.checks, i.evidence, i.created_at, i.updated_at, i.catalog_target, i.subject_item_id, i.dsh_session_id,
       i.pinned, i.archived
FROM studio_items i
JOIN studio_projects p ON p.tenant_id = i.tenant_id AND p.id = i.project_id
WHERE i.tenant_id = $1 AND i.project_id = $2 AND p.created_by = $3
ORDER BY i.pinned DESC, i.id DESC;

-- name: StudioItemGet :one
SELECT i.id, i.project_id, i.requirement_id, i.kind, i.title, i.stage,
       i.reference, i.reference_kind, i.checks, i.evidence, i.created_at, i.updated_at, i.catalog_target, i.subject_item_id, i.dsh_session_id,
       i.pinned, i.archived
FROM studio_items i
JOIN studio_projects p ON p.tenant_id = i.tenant_id AND p.id = i.project_id
WHERE i.tenant_id = $1 AND i.id = $2 AND p.created_by = $3;

-- name: StudioItemCreate :one
INSERT INTO studio_items (tenant_id, project_id, requirement_id, kind, title, checks, created_by, catalog_target, subject_item_id)
SELECT $1, p.id, sqlc.narg(requirement_id)::bigint, $3, $4, $5, $6, $7, sqlc.narg(subject_item_id)::bigint
FROM studio_projects p
WHERE p.tenant_id = $1 AND p.id = $2 AND p.created_by = $6
  AND (sqlc.narg(requirement_id)::bigint IS NULL OR EXISTS (
    SELECT 1 FROM studio_requirements r
    WHERE r.tenant_id = p.tenant_id AND r.project_id = p.id AND r.id = sqlc.narg(requirement_id) AND NOT r.archived
  ))
  AND (sqlc.narg(subject_item_id)::bigint IS NULL OR EXISTS (
    SELECT 1 FROM studio_items subject
    WHERE subject.tenant_id = p.tenant_id AND subject.project_id = p.id AND subject.id = sqlc.narg(subject_item_id)
      AND subject.kind IN ('capability','workflow','plugin') AND subject.catalog_target <> ''
  ))
RETURNING id, project_id, requirement_id, kind, title, stage,
          reference, reference_kind, checks, evidence, created_at, updated_at;

-- Pin a fixed definition version on an item that has not been accepted yet.
-- name: StudioItemBind :one
UPDATE studio_items i
SET reference = $3, reference_kind = $4, updated_at = now()
FROM studio_projects p
WHERE i.tenant_id = $1 AND i.id = $2 AND i.project_id = p.id
  AND p.created_by = $5 AND i.stage <> 'accepted'
RETURNING i.id, i.reference;

-- Deterministic stage transition. Acceptance additionally requires a non-empty
-- evidence document; the state CHECK keeps every other value out.
-- name: StudioItemTransition :one
UPDATE studio_items i
SET stage = $3,
    evidence = CASE WHEN $3 = 'accepted' THEN sqlc.arg(evidence)::jsonb ELSE i.evidence END,
    updated_at = now()
FROM studio_projects p
WHERE i.tenant_id = $1 AND i.id = $2 AND i.project_id = p.id AND p.created_by = $4
  AND i.stage <> $3
RETURNING i.id, i.stage, i.evidence;

-- Gateway invocation records, keyed by the caller-supplied idempotency key.

-- name: StudioRunUpsert :one
INSERT INTO studio_runs (tenant_id, project_id, mode, capability, version, request_key, status, invocation_id, approval_id, run_id)
SELECT $1, p.id, $3, $4, $5, $6, $7, $8, $9, $10
FROM studio_projects p
WHERE p.tenant_id = $1 AND p.id = $2 AND p.created_by = $11
ON CONFLICT (tenant_id, project_id, request_key) DO UPDATE
SET mode = EXCLUDED.mode, capability = EXCLUDED.capability, version = EXCLUDED.version,
    status = EXCLUDED.status, invocation_id = EXCLUDED.invocation_id,
    approval_id = EXCLUDED.approval_id, run_id = EXCLUDED.run_id, updated_at = now()
RETURNING id;

-- name: StudioRunList :many
SELECT r.id, r.project_id, r.mode, r.capability, r.version, r.request_key, r.status,
       r.invocation_id, r.approval_id, r.run_id, r.created_at, r.updated_at
FROM studio_runs r
JOIN studio_projects p ON p.tenant_id = r.tenant_id AND p.id = r.project_id
WHERE r.tenant_id = $1 AND r.project_id = $2 AND p.created_by = $3
ORDER BY r.id DESC;


-- Pin or unpin one execution session. Archived sessions cannot be pinned; the
-- join keeps the write inside the caller's owned project.
-- name: StudioItemSetPinned :one
UPDATE studio_items i
SET pinned = sqlc.arg(pinned), updated_at = now()
FROM studio_projects p
WHERE i.tenant_id = sqlc.arg(tenant_id) AND i.id = sqlc.arg(id) AND i.project_id = sqlc.arg(project)
  AND i.kind = 'execution' AND NOT i.archived
  AND p.tenant_id = i.tenant_id AND p.id = i.project_id AND p.created_by = sqlc.arg(created_by)
RETURNING i.id, i.pinned;

-- Archive or restore one execution session. Archiving clears the pin so a
-- restored session never returns pinned by surprise.
-- name: StudioItemSetArchived :one
UPDATE studio_items i
SET archived = sqlc.arg(archived),
    pinned = CASE WHEN sqlc.arg(archived)::boolean THEN false ELSE i.pinned END,
    updated_at = now()
FROM studio_projects p
WHERE i.tenant_id = sqlc.arg(tenant_id) AND i.id = sqlc.arg(id) AND i.project_id = sqlc.arg(project)
  AND i.kind = 'execution'
  AND p.tenant_id = i.tenant_id AND p.id = i.project_id AND p.created_by = sqlc.arg(created_by)
RETURNING i.id, i.archived, i.pinned;

-- name: StudioItemLinkSession :one
UPDATE studio_items i
SET dsh_session_id = sqlc.arg(session_id), updated_at = now()
FROM studio_projects p
WHERE i.tenant_id = sqlc.arg(tenant_id) AND i.id = sqlc.arg(item_id)
  AND i.kind = 'execution' AND i.dsh_session_id IS NULL
  AND p.tenant_id = i.tenant_id AND p.id = i.project_id AND p.created_by = sqlc.arg(created_by)
RETURNING i.dsh_session_id;

-- name: StudioSessionContext :one
SELECT i.id AS task_id, i.title AS task_title, i.stage, i.checks, i.reference,
       COALESCE(NULLIF(i.catalog_target, ''), subject.catalog_target, '') AS catalog_target,
       COALESCE(subject.kind, '') AS catalog_kind,
       p.id AS project_id, p.root AS project_root, p.development_brief,
       r.id AS requirement_id, r.parent_id AS requirement_parent_id,
       r.title AS requirement_title, r.description AS requirement_description,
       r.source AS requirement_source, r.archived AS requirement_archived
FROM studio_items i
JOIN studio_projects p ON p.tenant_id = i.tenant_id AND p.id = i.project_id
LEFT JOIN studio_requirements r ON r.tenant_id = i.tenant_id AND r.project_id = i.project_id AND r.id = i.requirement_id
LEFT JOIN studio_items subject ON subject.tenant_id = i.tenant_id AND subject.project_id = i.project_id AND subject.id = i.subject_item_id
WHERE i.tenant_id = $1 AND i.dsh_session_id = $2 AND i.kind = 'execution' AND p.created_by = $3;
