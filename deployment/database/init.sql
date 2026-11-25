-- Horizon deployment baseline for a new PostgreSQL schema.

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

COMMENT ON EXTENSION pg_trgm IS 'text similarity measurement and index searching based on trigrams';

SET default_tablespace = '';

SET default_table_access_method = heap;

CREATE TABLE iam_accounts (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    username text NOT NULL,
    status text NOT NULL,
    overrides jsonb DEFAULT '[]'::jsonb NOT NULL,
    is_admin boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_login timestamp with time zone,
    login_account_id uuid,
    machine_principal_id uuid,
    CONSTRAINT iam_accounts_overrides_check CHECK ((jsonb_typeof(overrides) = 'array'::text)),
    CONSTRAINT iam_accounts_status_check CHECK (status IN ('active', 'inactive')),
    CONSTRAINT iam_accounts_subject_reference CHECK (((login_account_id IS NULL) <> (machine_principal_id IS NULL)))
);

CREATE TABLE iam_principals (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    kind text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    display_name text DEFAULT ''::text NOT NULL,
    creation_source text DEFAULT 'system'::text NOT NULL,
    created_in_tenant text,
    security_notes text DEFAULT ''::text NOT NULL,
    risk_level text DEFAULT 'none'::text NOT NULL,
    CONSTRAINT iam_principals_kind_check CHECK (kind IN ('human', 'agent', 'service', 'application')),
    CONSTRAINT iam_principals_risk_level_check CHECK (risk_level IN ('none', 'elevated', 'high')),
    CONSTRAINT iam_principals_security_notes_check CHECK ((length(security_notes) <= 500)),
    CONSTRAINT iam_principals_status_check CHECK (status IN ('pending', 'active', 'suspended', 'inactive'))
);

CREATE TABLE identity_account_profiles (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    display_name text NOT NULL,
    email text DEFAULT ''::text NOT NULL
);

CREATE VIEW account_profile_directory AS
 SELECT account.id,
    account.tenant_id,
    account.username,
    COALESCE(profile.display_name, NULLIF(machine.display_name, ''::text), account.username) AS full_name,
    COALESCE(profile.email, ''::text) AS email,
    account.status,
    account.overrides,
    account.is_admin,
    account.created_at,
    account.last_login,
    account.login_account_id,
    account.machine_principal_id
   FROM ((iam_accounts account
     LEFT JOIN iam_principals machine ON ((machine.id = account.machine_principal_id)))
     LEFT JOIN identity_account_profiles profile ON (((profile.tenant_id = account.tenant_id) AND (profile.account_id = account.id))));

CREATE TABLE ai_configuration (
    tenant_id text NOT NULL,
    revision bigint NOT NULL,
    values_json jsonb NOT NULL,
    CONSTRAINT ai_configuration_revision_check CHECK ((revision > 0)),
    CONSTRAINT ai_configuration_values_json_check CHECK ((jsonb_typeof(values_json) = 'object'::text))
);

CREATE TABLE ai_conversations (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    session_id text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    archived_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ai_conversations_session_id_check CHECK (length(session_id) BETWEEN 1 AND 80),
    CONSTRAINT ai_conversations_title_check CHECK ((length(title) <= 200))
);

ALTER TABLE ai_conversations ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME ai_conversations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE ai_run_events (
    tenant_id text NOT NULL,
    run_id bigint NOT NULL,
    sequence bigint NOT NULL,
    event_type text NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE ai_runs (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    department_id bigint DEFAULT 0 NOT NULL,
    session_id text DEFAULT ''::text NOT NULL,
    agent text NOT NULL,
    model text NOT NULL,
    instruction_version text NOT NULL,
    question text NOT NULL,
    answer text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'running'::text NOT NULL,
    error_code text DEFAULT ''::text NOT NULL,
    tool_calls integer DEFAULT 0 NOT NULL,
    prompt_tokens integer DEFAULT 0 NOT NULL,
    completion_tokens integer DEFAULT 0 NOT NULL,
    duration_ms integer DEFAULT 0 NOT NULL,
    trace_id text NOT NULL,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    origin text DEFAULT 'human-ui'::text NOT NULL,
    correlation_id text DEFAULT ''::text NOT NULL,
    causation_id text DEFAULT ''::text NOT NULL,
    trigger_depth integer DEFAULT 0 NOT NULL,
    trigger_policy text DEFAULT ''::text NOT NULL,
    event_id text DEFAULT ''::text NOT NULL,
    identity_binding_id uuid,
    CONSTRAINT ai_runs_completion_tokens_check CHECK ((completion_tokens >= 0)),
    CONSTRAINT ai_runs_duration_ms_check CHECK ((duration_ms >= 0)),
    CONSTRAINT ai_runs_prompt_tokens_check CHECK ((prompt_tokens >= 0)),
    CONSTRAINT ai_runs_question_check CHECK (length(question) BETWEEN 1 AND 4000),
    CONSTRAINT ai_runs_status_check CHECK (status IN ('running', 'completed', 'awaiting_approval', 'failed', 'cancelled')),
    CONSTRAINT ai_runs_tool_calls_check CHECK ((tool_calls >= 0)),
    CONSTRAINT orchestration_actor_authority CHECK (((agent <> 'orchestration'::text) OR (department_id = 0)))
);

ALTER TABLE ai_runs ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME ai_runs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE ai_trigger_executions (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    policy_id text NOT NULL,
    policy_version integer NOT NULL,
    event_id text NOT NULL,
    run_id bigint,
    status text NOT NULL,
    error_code text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    CONSTRAINT ai_trigger_executions_status_check CHECK (status IN ('queued', 'running', 'completed', 'failed', 'skipped'))
);

ALTER TABLE ai_trigger_executions ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME ai_trigger_executions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE ai_trigger_policies (
    id bigint NOT NULL,
    tenant_id text DEFAULT '*'::text NOT NULL,
    policy_id text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    event_type text NOT NULL,
    agent text NOT NULL,
    mode text DEFAULT 'async'::text NOT NULL,
    instruction text DEFAULT ''::text NOT NULL,
    max_depth integer DEFAULT 3 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ai_trigger_policies_max_depth_check CHECK (((max_depth >= 1) AND (max_depth <= 10))),
    CONSTRAINT ai_trigger_policies_mode_check CHECK (mode IN ('async', 'event')),
    CONSTRAINT ai_trigger_policies_version_check CHECK ((version > 0))
);

ALTER TABLE ai_trigger_policies ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME ai_trigger_policies_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE articles (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    created_by bigint NOT NULL,
    title text NOT NULL,
    body text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    review_code text DEFAULT ''::text NOT NULL,
    reviewed_by bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT articles_body_check CHECK (length(body) BETWEEN 1 AND 20000),
    CONSTRAINT articles_status_check CHECK (status IN ('draft', 'pending_ai', 'pending_review', 'published', 'rejected')),
    CONSTRAINT articles_title_check CHECK (length(title) BETWEEN 1 AND 200)
);

ALTER TABLE articles ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME articles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE automation_runs (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    task_id bigint NOT NULL,
    occurrence_key text NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    trigger_kind text DEFAULT 'schedule'::text NOT NULL,
    status text NOT NULL,
    error_code text DEFAULT ''::text NOT NULL,
    ai_run_id bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    service_execution text,
    CONSTRAINT automation_runs_occurrence_key_check CHECK (length(occurrence_key) BETWEEN 1 AND 120),
    CONSTRAINT automation_runs_status_check CHECK (status IN ('queued', 'running', 'awaiting_approval', 'completed', 'failed', 'skipped', 'cancelled')),
    CONSTRAINT automation_runs_trigger_kind_check CHECK (trigger_kind IN ('schedule', 'manual'))
);

ALTER TABLE automation_runs ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME automation_runs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE automation_tasks (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    owner_id bigint NOT NULL,
    department_id bigint DEFAULT 0 NOT NULL,
    name text NOT NULL,
    description text NOT NULL,
    schedule_kind text NOT NULL,
    run_at timestamp with time zone NOT NULL,
    timezone text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    next_run_at timestamp with time zone,
    last_run_at timestamp with time zone,
    failure_count integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    schedule_day integer,
    identity_binding_id uuid,
    service_execution text,
    CONSTRAINT automation_tasks_description_check CHECK (length(description) BETWEEN 1 AND 4000),
    CONSTRAINT automation_tasks_failure_count_check CHECK ((failure_count >= 0)),
    CONSTRAINT automation_tasks_name_check CHECK (length(name) BETWEEN 1 AND 120),
    CONSTRAINT automation_tasks_schedule_day_check CHECK (((schedule_day >= 1) AND (schedule_day <= 31))),
    CONSTRAINT automation_tasks_schedule_kind_check CHECK (schedule_kind IN ('once', 'hourly', 'daily', 'weekly', 'monthly')),
    CONSTRAINT automation_tasks_timezone_check CHECK (length(timezone) BETWEEN 1 AND 100)
);

ALTER TABLE automation_tasks ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME automation_tasks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE cap_approvals (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    capability text NOT NULL,
    capability_version text NOT NULL,
    invocation_id text DEFAULT ''::text NOT NULL,
    risk text NOT NULL,
    approval text NOT NULL,
    arguments jsonb NOT NULL,
    arguments_digest text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    requested_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    decided_by bigint,
    decided_at timestamp with time zone,
    decision_note text DEFAULT ''::text NOT NULL,
    execution_code text DEFAULT ''::text NOT NULL,
    action_key text DEFAULT ''::text NOT NULL,
    identity_binding_id uuid,
    CONSTRAINT cap_approvals_approval_check CHECK (approval IN ('confirm', 'four-eyes')),
    CONSTRAINT cap_approvals_status_check CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'executed', 'failed'))
);

ALTER TABLE cap_approvals ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME cap_approvals_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE cap_events (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    event_id text NOT NULL,
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    invocation_id text DEFAULT ''::text NOT NULL,
    account_id bigint DEFAULT 0 NOT NULL,
    correlation_id text DEFAULT ''::text NOT NULL,
    causation_id text DEFAULT ''::text NOT NULL,
    trigger_depth integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    identity_binding_id uuid,
    CONSTRAINT cap_events_trigger_depth_check CHECK ((trigger_depth >= 0))
);

ALTER TABLE cap_events ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME cap_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE cap_execution_binding_changes (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    capability text NOT NULL,
    capability_version text NOT NULL,
    plan_id text NOT NULL,
    plan_version bigint NOT NULL,
    revision bigint NOT NULL,
    action text NOT NULL,
    reason text NOT NULL,
    account_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE cap_execution_binding_changes ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME cap_execution_binding_changes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE cap_execution_bindings (
    tenant_id text NOT NULL,
    capability text NOT NULL,
    capability_version text NOT NULL,
    plan_tenant_id text NOT NULL,
    plan_id text NOT NULL,
    plan_version bigint NOT NULL,
    revision bigint NOT NULL,
    updated_by bigint NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT cap_execution_bindings_check CHECK (((plan_tenant_id = ''::text) OR (plan_tenant_id = tenant_id))),
    CONSTRAINT cap_execution_bindings_revision_check CHECK ((revision > 0))
);

CREATE TABLE cap_execution_plans (
    tenant_id text NOT NULL,
    plan_id text NOT NULL,
    version bigint NOT NULL,
    capability text NOT NULL,
    capability_version text NOT NULL,
    name text NOT NULL,
    mode text NOT NULL,
    implementation_ref text NOT NULL,
    contract_hash text NOT NULL,
    status text NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    published_by bigint DEFAULT 0 NOT NULL,
    published_at timestamp with time zone,
    workflow_hash text DEFAULT ''::text NOT NULL,
    CONSTRAINT cap_execution_plans_mode_check CHECK (mode IN ('direct', 'workflow', 'graph')),
    CONSTRAINT cap_execution_plans_status_check CHECK (status IN ('draft', 'validated', 'published', 'archived')),
    CONSTRAINT cap_execution_plans_version_check CHECK ((version > 0))
);

CREATE TABLE cap_execution_records (
    tenant_id text NOT NULL,
    invocation_id text NOT NULL,
    account_id bigint NOT NULL,
    binding jsonb NOT NULL,
    run_id bigint,
    actor_context jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT cap_execution_records_actor_context_check CHECK ((jsonb_typeof(actor_context) = 'object'::text)),
    CONSTRAINT cap_execution_records_binding_check CHECK ((jsonb_typeof(binding) = 'object'::text))
);

CREATE TABLE cap_invocations (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    invocation_id text NOT NULL,
    capability text NOT NULL,
    capability_version text NOT NULL,
    origin text NOT NULL,
    idempotency_key text DEFAULT ''::text NOT NULL,
    status text NOT NULL,
    input jsonb DEFAULT '{}'::jsonb NOT NULL,
    input_digest text NOT NULL,
    result jsonb,
    error_code text DEFAULT ''::text NOT NULL,
    approval_id bigint DEFAULT 0 NOT NULL,
    correlation_id text DEFAULT ''::text NOT NULL,
    causation_id text DEFAULT ''::text NOT NULL,
    trigger_depth integer DEFAULT 0 NOT NULL,
    duration_ms integer DEFAULT 0 NOT NULL,
    trace_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    identity_binding_id uuid,
    machine_credential_id uuid,
    agent_delegation_id uuid,
    CONSTRAINT cap_invocations_duration_ms_check CHECK ((duration_ms >= 0)),
    CONSTRAINT cap_invocations_origin_check CHECK (origin IN ('human-ui', 'agent', 'workflow', 'api', 'event', 'approval', 'capability', 'automation')),
    CONSTRAINT cap_invocations_status_check CHECK (status IN ('requested', 'awaiting_approval', 'executing', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT cap_invocations_trigger_depth_check CHECK ((trigger_depth >= 0))
);

ALTER TABLE cap_invocations ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME cap_invocations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE cap_outbox (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    event_id text NOT NULL,
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    invocation_id text DEFAULT ''::text NOT NULL,
    account_id bigint DEFAULT 0 NOT NULL,
    correlation_id text DEFAULT ''::text NOT NULL,
    causation_id text DEFAULT ''::text NOT NULL,
    trigger_depth integer DEFAULT 0 NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    available_at timestamp with time zone DEFAULT now() NOT NULL,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    identity_binding_id uuid,
    CONSTRAINT cap_outbox_attempts_check CHECK ((attempts >= 0)),
    CONSTRAINT cap_outbox_status_check CHECK (status IN ('pending', 'published', 'failed')),
    CONSTRAINT cap_outbox_trigger_depth_check CHECK ((trigger_depth >= 0))
);

ALTER TABLE cap_outbox ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME cap_outbox_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE drive_files (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    space_id bigint NOT NULL,
    folder_id bigint,
    file_id bigint NOT NULL,
    name text NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    trash_batch_id bigint,
    trash_root boolean DEFAULT false NOT NULL,
    CONSTRAINT drive_files_check CHECK ((((deleted_at IS NULL) AND (trash_batch_id IS NULL) AND (NOT trash_root)) OR ((deleted_at IS NOT NULL) AND (trash_batch_id IS NOT NULL)))),
    CONSTRAINT drive_files_name_check CHECK (char_length(name) BETWEEN 1 AND 200)
);

ALTER TABLE drive_files ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME drive_files_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE drive_folders (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    space_id bigint NOT NULL,
    parent_id bigint,
    name text NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    trash_batch_id bigint,
    trash_root boolean DEFAULT false NOT NULL,
    CONSTRAINT drive_folders_check CHECK ((((deleted_at IS NULL) AND (trash_batch_id IS NULL) AND (NOT trash_root)) OR ((deleted_at IS NOT NULL) AND (trash_batch_id IS NOT NULL)))),
    CONSTRAINT drive_folders_name_check CHECK (char_length(name) BETWEEN 1 AND 200)
);

ALTER TABLE drive_folders ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME drive_folders_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE drive_members (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    space_id bigint NOT NULL,
    account_id bigint,
    department_id bigint,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT drive_members_check CHECK (((account_id IS NOT NULL) <> (department_id IS NOT NULL))),
    CONSTRAINT drive_members_role_check CHECK (role IN ('admin', 'editor', 'viewer'))
);

ALTER TABLE drive_members ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME drive_members_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE drive_spaces (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    kind text NOT NULL,
    name text NOT NULL,
    owner_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT drive_spaces_kind_check CHECK (kind IN ('personal', 'shared')),
    CONSTRAINT drive_spaces_name_check CHECK (char_length(name) BETWEEN 1 AND 120)
);

ALTER TABLE drive_spaces ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME drive_spaces_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE drive_trash_batches (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    space_id bigint NOT NULL,
    deleted_by bigint NOT NULL,
    deleted_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone DEFAULT (now() + '60 days'::interval) NOT NULL,
    purging_at timestamp with time zone
);

ALTER TABLE drive_trash_batches ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME drive_trash_batches_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE drive_upload_chunks (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    upload_id bigint NOT NULL,
    chunk_index integer NOT NULL,
    storage_key text NOT NULL,
    expected_size bigint NOT NULL,
    sha256 text,
    uploaded_at timestamp with time zone,
    CONSTRAINT drive_upload_chunks_check CHECK ((((sha256 IS NULL) AND (uploaded_at IS NULL)) OR ((sha256 IS NOT NULL) AND (uploaded_at IS NOT NULL)))),
    CONSTRAINT drive_upload_chunks_chunk_index_check CHECK (((chunk_index >= 0) AND (chunk_index <= 255))),
    CONSTRAINT drive_upload_chunks_expected_size_check CHECK (((expected_size >= 1) AND (expected_size <= 4194304)))
);

ALTER TABLE drive_upload_chunks ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME drive_upload_chunks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE drive_uploads (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    space_id bigint NOT NULL,
    folder_id bigint,
    owner_id bigint NOT NULL,
    storage_profile_id bigint NOT NULL,
    object_key text NOT NULL,
    name text NOT NULL,
    size bigint NOT NULL,
    state text DEFAULT 'open'::text NOT NULL,
    file_id bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone DEFAULT (now() + '24:00:00'::interval) NOT NULL,
    CONSTRAINT drive_uploads_name_check CHECK (char_length(name) BETWEEN 1 AND 200),
    CONSTRAINT drive_uploads_size_check CHECK (((size >= 0) AND (size <= 1073741824))),
    CONSTRAINT drive_uploads_state_check CHECK (state IN ('open', 'completed', 'cancelled', 'cleaned'))
);

ALTER TABLE drive_uploads ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME drive_uploads_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE example_orders (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    supplier_id bigint NOT NULL,
    title character varying(200) NOT NULL,
    CONSTRAINT example_orders_title_check CHECK ((length((title)::text) > 0))
);

CREATE TABLE example_records (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    title text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT example_records_status_check CHECK (status IN ('draft', 'active', 'archived')),
    CONSTRAINT example_records_title_check CHECK (length(title) BETWEEN 1 AND 200)
);

CREATE VIEW example_accessible_orders AS
 SELECT orders.id, orders.tenant_id, orders.supplier_id, orders.title
 FROM example_orders orders
 JOIN example_records supplier ON supplier.tenant_id = orders.tenant_id AND supplier.id = orders.supplier_id
 WHERE supplier.status <> 'archived';

ALTER TABLE example_orders ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME example_orders_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

ALTER TABLE example_records ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME example_records_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE gov_announcement_files (
    tenant_id text NOT NULL,
    announcement_id bigint NOT NULL,
    file_id bigint NOT NULL
);

CREATE TABLE gov_announcements (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    sender_id bigint NOT NULL,
    department_id bigint NOT NULL,
    client_key text NOT NULL,
    payload_hash text NOT NULL,
    title text NOT NULL,
    markdown text NOT NULL,
    targets jsonb NOT NULL,
    email boolean NOT NULL,
    recipient_count integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE gov_announcements ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME gov_announcements_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE gov_audit (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    scope_key text NOT NULL,
    actor_id bigint NOT NULL,
    action text NOT NULL,
    resource_key text NOT NULL,
    revision bigint NOT NULL,
    detail jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE gov_audit ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME gov_audit_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE gov_deliveries (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    message_id bigint NOT NULL,
    channel text DEFAULT 'smtp'::text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    error_code text DEFAULT ''::text NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    lease_until timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT gov_deliveries_status_check CHECK (status IN ('pending', 'processing', 'sent', 'failed', 'skipped'))
);

ALTER TABLE gov_deliveries ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME gov_deliveries_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE gov_dictionaries (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    scope_key text NOT NULL,
    code text NOT NULL,
    spec jsonb NOT NULL,
    revision bigint DEFAULT 0 NOT NULL
);

ALTER TABLE gov_dictionaries ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME gov_dictionaries_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE gov_dictionary_items (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    dictionary_id bigint NOT NULL,
    scope_key text NOT NULL,
    code text NOT NULL,
    parent_code text DEFAULT ''::text NOT NULL,
    spec jsonb NOT NULL
);

ALTER TABLE gov_dictionary_items ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME gov_dictionary_items_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE gov_dictionary_overrides (
    tenant_id text NOT NULL,
    dictionary_id bigint NOT NULL,
    item_id bigint NOT NULL,
    scope_key text NOT NULL,
    spec jsonb NOT NULL
);

CREATE TABLE gov_dictionary_references (
    tenant_id text NOT NULL,
    dictionary_id bigint NOT NULL,
    item_id bigint NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL
);

CREATE TABLE gov_drafts (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    domain text NOT NULL,
    resource_key text NOT NULL,
    scope_key text NOT NULL,
    entity_id bigint DEFAULT 0 NOT NULL,
    base_revision bigint NOT NULL,
    payload jsonb NOT NULL,
    reason text NOT NULL,
    sensitive boolean NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    actor_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT gov_drafts_domain_check CHECK (domain IN ('settings', 'dictionary', 'override')),
    CONSTRAINT gov_drafts_reason_check CHECK (char_length(reason) BETWEEN 5 AND 500),
    CONSTRAINT gov_drafts_status_check CHECK (status IN ('draft', 'published'))
);

ALTER TABLE gov_drafts ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME gov_drafts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE gov_events (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    request_id bigint NOT NULL,
    event text NOT NULL,
    processed boolean DEFAULT false NOT NULL,
    lease_until timestamp with time zone,
    attempts integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE gov_events ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME gov_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE gov_file_bindings (
    tenant_id text NOT NULL,
    file_id bigint NOT NULL,
    scope_key text NOT NULL,
    revision bigint NOT NULL
);

CREATE TABLE gov_files (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    owner_id bigint NOT NULL,
    organization_id bigint NOT NULL,
    department_id bigint NOT NULL,
    storage_profile_id bigint NOT NULL,
    storage_key text NOT NULL,
    name text NOT NULL,
    mime text NOT NULL,
    size bigint NOT NULL,
    purpose text NOT NULL,
    sha256 text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    purged_at timestamp with time zone,
    CONSTRAINT gov_files_purpose_check CHECK ((purpose ~ '^[a-z][a-z0-9_]{0,63}$'::text)),
    CONSTRAINT gov_files_size_check CHECK (((size >= 0) AND ((purpose = 'cloud_drive'::text) OR (size > 0))))
);

ALTER TABLE gov_files ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME gov_files_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE gov_identity_refs (
    tenant_id text NOT NULL,
    account_id bigint NOT NULL
);

CREATE TABLE identity_department_memberships (
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    organization_id bigint NOT NULL,
    department_id bigint NOT NULL,
    overrides jsonb DEFAULT '[]'::jsonb NOT NULL,
    CONSTRAINT identity_department_memberships_overrides_check CHECK ((jsonb_typeof(overrides) = 'array'::text))
);

CREATE TABLE identity_departments (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    organization_id bigint NOT NULL,
    parent_id bigint,
    name text NOT NULL,
    code text NOT NULL,
    manager text DEFAULT ''::text NOT NULL,
    status text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT identity_departments_name_check CHECK (char_length(name) BETWEEN 1 AND 100),
    CONSTRAINT identity_departments_status_check CHECK (status IN ('active', 'inactive'))
);

CREATE TABLE identity_organizations (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    name text NOT NULL,
    code text NOT NULL,
    kind text NOT NULL,
    parent_id bigint,
    status text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT identity_organizations_root_check CHECK (parent_id IS NULL),
    CONSTRAINT identity_organizations_kind_check CHECK (kind IN ('company')),
    CONSTRAINT identity_organizations_name_check CHECK (char_length(name) BETWEEN 1 AND 100),
    CONSTRAINT identity_organizations_status_check CHECK (status IN ('active', 'inactive'))
);

CREATE VIEW gov_notification_members AS
 SELECT a.tenant_id, a.id AS account_id, a.username AS full_name, m.department_id, m.organization_id
 FROM iam_accounts a
 JOIN identity_department_memberships m ON m.tenant_id = a.tenant_id AND m.account_id = a.id
 JOIN identity_departments d ON d.tenant_id = m.tenant_id AND d.id = m.department_id AND d.status = 'active'
 JOIN identity_organizations o ON o.tenant_id = m.tenant_id AND o.id = m.organization_id AND o.status = 'active'
 WHERE a.status = 'active';

CREATE VIEW gov_notification_targets AS
 WITH RECURSIVE paths AS (
         SELECT identity_departments.tenant_id,
            identity_departments.id AS ancestor,
            identity_departments.id
           FROM identity_departments
          WHERE (identity_departments.status = 'active'::text)
        UNION ALL
         SELECT p.tenant_id,
            p.ancestor,
            d.id
           FROM (paths p
             JOIN identity_departments d ON (((d.tenant_id = p.tenant_id) AND (d.parent_id = p.id) AND (d.status = 'active'::text))))
        ), members AS (
         SELECT m.tenant_id,
            m.account_id,
            m.full_name,
            m.department_id,
            m.organization_id,
            o.name AS company_name
           FROM gov_notification_members m
           JOIN identity_organizations o ON o.tenant_id = m.tenant_id AND o.id = m.organization_id
        )
 SELECT members.tenant_id,
    'account'::text AS kind,
    members.account_id AS target_id,
    members.full_name AS name,
    members.account_id,
    members.department_id,
    members.organization_id
   FROM members
UNION ALL
 SELECT m.tenant_id,
    'department'::text AS kind,
    p.ancestor AS target_id,
    d.name,
    m.account_id,
    m.department_id,
    m.organization_id
   FROM ((members m
     JOIN paths p ON (((p.tenant_id = m.tenant_id) AND (p.id = m.department_id))))
     JOIN identity_departments d ON (((d.tenant_id = p.tenant_id) AND (d.id = p.ancestor))))
UNION ALL
 SELECT members.tenant_id,
    'company'::text AS kind,
    members.organization_id AS target_id,
    members.company_name AS name,
    members.account_id,
    members.department_id,
    members.organization_id
   FROM members;

CREATE TABLE gov_notifications (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    event_id bigint,
    recipient_id bigint NOT NULL,
    department_id bigint,
    payload jsonb NOT NULL,
    read_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    announcement_id bigint,
    CONSTRAINT gov_announcement_department CHECK (((announcement_id IS NULL) OR (department_id IS NOT NULL))),
    CONSTRAINT gov_notifications_check CHECK (((event_id IS NULL) <> (announcement_id IS NULL)))
);

ALTER TABLE gov_notifications ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME gov_notifications_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE gov_revisions (
    tenant_id text NOT NULL,
    resource_key text NOT NULL,
    scope_key text NOT NULL,
    revision bigint DEFAULT 0 NOT NULL
);

CREATE TABLE gov_scopes (
    tenant_id text NOT NULL,
    scope_key text NOT NULL,
    kind text NOT NULL,
    entity_id bigint NOT NULL,
    organization_id bigint,
    department_id bigint,
    account_id bigint,
    CONSTRAINT gov_scopes_check CHECK ((scope_key = ((kind || ':'::text) || (entity_id)::text))),
    CONSTRAINT gov_scopes_check1 CHECK ((((kind = 'workspace'::text) AND (entity_id = 0) AND (organization_id IS NULL) AND (department_id IS NULL) AND (account_id IS NULL)) OR ((kind = ANY (ARRAY['enterprise'::text, 'company'::text])) AND (organization_id IS NOT NULL) AND (entity_id = organization_id) AND (department_id IS NULL) AND (account_id IS NULL)) OR ((kind = 'department'::text) AND (department_id IS NOT NULL) AND (entity_id = department_id) AND (organization_id IS NULL) AND (account_id IS NULL)) OR ((kind = 'account'::text) AND (account_id IS NOT NULL) AND (entity_id = account_id) AND (organization_id IS NULL) AND (department_id IS NULL)))),
    CONSTRAINT gov_scopes_kind_check CHECK (kind IN ('workspace', 'enterprise', 'company', 'department', 'account'))
);

CREATE TABLE gov_settings_schedules (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    draft_id bigint NOT NULL,
    actor_id bigint NOT NULL,
    department_id bigint NOT NULL,
    base_revision bigint NOT NULL,
    draft_hash text NOT NULL,
    sensitive_confirmed boolean NOT NULL,
    scheduled_at timestamp with time zone NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    error_code text DEFAULT ''::text NOT NULL,
    version_id bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    identity_binding_id uuid,
    CONSTRAINT gov_settings_schedules_base_revision_check CHECK ((base_revision >= 0)),
    CONSTRAINT gov_settings_schedules_check CHECK (((status = 'published'::text) = (version_id IS NOT NULL))),
    CONSTRAINT gov_settings_schedules_check1 CHECK (((status = 'pending'::text) = (finished_at IS NULL))),
    CONSTRAINT gov_settings_schedules_status_check CHECK (status IN ('pending', 'published', 'cancelled', 'failed'))
);

ALTER TABLE gov_settings_schedules ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME gov_settings_schedules_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE gov_storage_profiles (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    driver text NOT NULL,
    config_hash text NOT NULL,
    config jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE gov_storage_profiles ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME gov_storage_profiles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE gov_values (
    tenant_id text NOT NULL,
    scope_key text NOT NULL,
    config_key text NOT NULL,
    value_json jsonb NOT NULL
);

CREATE TABLE gov_versions (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    domain text NOT NULL,
    resource_key text NOT NULL,
    scope_key text NOT NULL,
    entity_id bigint DEFAULT 0 NOT NULL,
    revision bigint NOT NULL,
    payload jsonb NOT NULL,
    reason text NOT NULL,
    actor_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE gov_versions ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME gov_versions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE iam_access_reviews (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    principal_id uuid NOT NULL,
    kind text NOT NULL,
    target_id uuid NOT NULL,
    role_id bigint,
    target_expires_at timestamp with time zone,
    target_revoked_at timestamp with time zone,
    status text DEFAULT 'pending'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    reviewer_id bigint,
    reviewed_at timestamp with time zone,
    reason text,
    CONSTRAINT iam_access_reviews_kind_check CHECK (kind IN ('membership', 'context_role')),
    CONSTRAINT iam_access_reviews_role_check CHECK (((kind = 'context_role'::text) = (role_id IS NOT NULL))),
    CONSTRAINT iam_access_reviews_status_check CHECK (status IN ('pending', 'retained', 'revoked', 'superseded'))
);

ALTER TABLE iam_access_reviews ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME iam_access_reviews_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE iam_account_roles (
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    role_id bigint NOT NULL,
    id uuid DEFAULT gen_random_uuid() NOT NULL
);

ALTER TABLE iam_accounts ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME iam_accounts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE iam_login_accounts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    principal_id uuid NOT NULL,
    username text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    realm text DEFAULT 'local'::text NOT NULL,
    CONSTRAINT iam_login_accounts_realm_check CHECK ((realm ~ '^[a-z0-9][a-z0-9_-]{0,62}$'::text)),
    CONSTRAINT iam_login_accounts_status_check CHECK (status IN ('active', 'frozen', 'revoked'))
);

CREATE TABLE iam_tenant_memberships (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id text NOT NULL,
    principal_id uuid NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    expires_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    access_revoked_at timestamp with time zone,
    CONSTRAINT iam_tenant_memberships_status_check CHECK (status IN ('pending', 'active', 'suspended', 'revoked'))
);

CREATE TABLE iam_tenants (
    id text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    review_interval_days integer DEFAULT 90 NOT NULL,
    next_review_at timestamp with time zone DEFAULT (now() + '90 days'::interval) NOT NULL,
    registration_channel text DEFAULT 'invite'::text NOT NULL,
    resource_revision bigint DEFAULT 0 NOT NULL,
    audit_export_approval boolean DEFAULT false NOT NULL,
    CONSTRAINT iam_tenants_registration_channel_check CHECK (registration_channel IN ('invite', 'email', 'sms')),
    CONSTRAINT iam_tenants_review_interval_days_check CHECK (((review_interval_days >= 1) AND (review_interval_days <= 365))),
    CONSTRAINT iam_tenants_status_check CHECK (status IN ('active', 'inactive'))
);

CREATE VIEW iam_active_account_subjects AS
 SELECT placement.tenant_id,
    placement.id AS account_id,
    login.username,
    login.id AS login_account_id,
    subject.id AS principal_id,
    member.id AS membership_id
   FROM ((((iam_accounts placement
     JOIN iam_login_accounts login ON (((login.id = placement.login_account_id) AND (login.status = 'active'::text))))
     JOIN iam_principals subject ON (((subject.id = login.principal_id) AND (subject.status = 'active'::text) AND (subject.kind = 'human'::text))))
     JOIN iam_tenants tenant ON (((tenant.id = placement.tenant_id) AND (tenant.status = 'active'::text))))
     JOIN iam_tenant_memberships member ON (((member.tenant_id = tenant.id) AND (member.principal_id = subject.id) AND (member.status = 'active'::text) AND ((member.expires_at IS NULL) OR (member.expires_at > now())))))
  WHERE (placement.status = 'active'::text);

CREATE TABLE iam_identity_bindings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id text NOT NULL,
    principal_id uuid NOT NULL,
    identity_type text NOT NULL,
    identity_version text NOT NULL,
    revocation_event text NOT NULL,
    entity_id bigint NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    verification_level text DEFAULT 'provider'::text NOT NULL,
    source text DEFAULT 'claim'::text NOT NULL,
    valid_until timestamp with time zone,
    CONSTRAINT iam_identity_bindings_entity_id_check CHECK ((entity_id > 0)),
    CONSTRAINT iam_identity_bindings_status_check CHECK (status IN ('pending', 'active', 'suspended', 'expired', 'revoked')),
    CONSTRAINT iam_identity_bindings_validity CHECK (((valid_until IS NULL) OR (valid_until > created_at)))
);

CREATE VIEW iam_active_identity_bindings AS
 SELECT id,
    tenant_id,
    principal_id,
    identity_type,
    identity_version,
    entity_id
   FROM iam_identity_bindings binding
  WHERE ((status = 'active'::text) AND ((valid_until IS NULL) OR (valid_until > statement_timestamp())) AND (NOT (EXISTS ( SELECT 1
           FROM cap_outbox event
          WHERE ((event.tenant_id = binding.tenant_id) AND (event.event_type = binding.revocation_event) AND (event.aggregate_id = (binding.entity_id)::text))))));

CREATE VIEW iam_active_machine_subjects AS
 SELECT account.tenant_id,
    account.id AS account_id,
    principal.id AS principal_id,
    principal.kind,
    membership.id AS membership_id
   FROM (((iam_accounts account
     JOIN iam_principals principal ON (((principal.id = account.machine_principal_id) AND (principal.status = 'active'::text) AND (principal.kind <> 'human'::text))))
     JOIN iam_tenants tenant ON (((tenant.id = account.tenant_id) AND (tenant.status = 'active'::text))))
     JOIN iam_tenant_memberships membership ON (((membership.tenant_id = account.tenant_id) AND (membership.principal_id = principal.id) AND (membership.status = 'active'::text) AND ((membership.expires_at IS NULL) OR (membership.expires_at > now())))))
  WHERE (account.status = 'active'::text);

CREATE TABLE iam_agent_delegations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id text NOT NULL,
    agent_account_id bigint NOT NULL,
    delegator_account_id bigint NOT NULL,
    delegator_binding_id uuid,
    capability text NOT NULL,
    resource_id bigint NOT NULL,
    max_risk text NOT NULL,
    remaining_uses integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    additional_capabilities jsonb DEFAULT '[]'::jsonb NOT NULL,
    CONSTRAINT iam_agent_delegations_additional_capabilities_check CHECK (((jsonb_typeof(additional_capabilities) = 'array'::text) AND (jsonb_array_length(additional_capabilities) <= 19))),
    CONSTRAINT iam_agent_delegations_check CHECK ((expires_at > created_at)),
    CONSTRAINT iam_agent_delegations_max_risk_check CHECK (max_risk IN ('low', 'medium', 'high')),
    CONSTRAINT iam_agent_delegations_remaining_uses_check CHECK ((remaining_uses >= 0)),
    CONSTRAINT iam_agent_delegations_resource_id_check CHECK ((resource_id > 0))
);

CREATE TABLE iam_audit_export_permits (
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    membership_id uuid NOT NULL,
    filter_digest text NOT NULL,
    filters jsonb NOT NULL,
    request_id text NOT NULL,
    binding_id text NOT NULL,
    expires_at timestamp with time zone DEFAULT (now() + '00:15:00'::interval) NOT NULL,
    CONSTRAINT iam_audit_export_permits_filters_check CHECK ((jsonb_typeof(filters) = 'object'::text))
);

CREATE TABLE iam_contact_challenges (
    id uuid NOT NULL,
    login_account_id uuid NOT NULL,
    channel text NOT NULL,
    destination text NOT NULL,
    purpose text NOT NULL,
    token_hash text NOT NULL,
    session_hash text DEFAULT ''::text NOT NULL,
    context_hash text DEFAULT ''::text NOT NULL,
    sealed_token text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    delivery_due_at timestamp with time zone DEFAULT now() NOT NULL,
    delivery_attempts integer DEFAULT 0 NOT NULL,
    delivered_at timestamp with time zone,
    consumed_at timestamp with time zone,
    CONSTRAINT iam_contact_challenges_channel_check CHECK (channel IN ('email', 'sms')),
    CONSTRAINT iam_contact_challenges_purpose_check CHECK (purpose IN ('verify', 'password_reset', 'registration'))
);

CREATE TABLE iam_context_role_grants (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id text NOT NULL,
    principal_id uuid NOT NULL,
    identity_binding_id uuid,
    role_id bigint NOT NULL,
    granted_by bigint NOT NULL,
    reason text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    CONSTRAINT iam_context_role_grants_check CHECK ((expires_at > created_at)),
    CONSTRAINT iam_context_role_grants_reason_check CHECK (char_length(reason) BETWEEN 5 AND 500)
);

CREATE VIEW identity_department_directory AS
 SELECT id, tenant_id, organization_id, parent_id, name, code, manager, status, description, created_at
 FROM identity_departments;

CREATE TABLE identity_department_membership_roles (
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    department_id bigint NOT NULL,
    role_id bigint NOT NULL,
    id uuid DEFAULT gen_random_uuid() NOT NULL
);

ALTER TABLE identity_departments ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME identity_departments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE iam_disabled_resources (
    tenant_id text NOT NULL,
    resource text NOT NULL
);

CREATE TABLE iam_external_identities (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    provider_id text NOT NULL,
    issuer text NOT NULL,
    subject text NOT NULL,
    login_account_id uuid NOT NULL,
    linked_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone
);

CREATE TABLE iam_external_identity_proofs (
    proof_hash text NOT NULL,
    provider_id text NOT NULL,
    issuer text NOT NULL,
    subject text NOT NULL,
    expires_at timestamp with time zone DEFAULT (now() + '00:05:00'::interval) NOT NULL,
    consumed_at timestamp with time zone,
    purpose text DEFAULT 'link'::text NOT NULL,
    completion boolean DEFAULT false NOT NULL,
    CONSTRAINT iam_external_identity_proofs_purpose_check CHECK (purpose IN ('login', 'link'))
);

CREATE TABLE iam_external_identity_reviews (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id text NOT NULL,
    external_identity_id uuid NOT NULL,
    original_login_id uuid NOT NULL,
    requested_login_id uuid NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone DEFAULT (now() + '1 day'::interval) NOT NULL,
    decided_by uuid,
    reason text,
    applied_at timestamp with time zone,
    original_linked_at timestamp with time zone NOT NULL,
    CONSTRAINT iam_external_identity_reviews_check CHECK ((original_login_id <> requested_login_id)),
    CONSTRAINT iam_external_identity_reviews_status_check CHECK (status IN ('pending', 'confirmed', 'applied', 'rejected', 'rolled_back'))
);

CREATE TABLE iam_federation_assertion_uses (
    provider_id text NOT NULL,
    assertion_hash text NOT NULL,
    expires_at timestamp with time zone NOT NULL
);

CREATE TABLE iam_federation_flows (
    state_hash text NOT NULL,
    browser_hash text NOT NULL,
    provider_id text NOT NULL,
    issuer text NOT NULL,
    purpose text NOT NULL,
    nonce_hash text NOT NULL,
    sealed_verifier text NOT NULL,
    expires_at timestamp with time zone DEFAULT (now() + '00:05:00'::interval) NOT NULL,
    consumed_at timestamp with time zone,
    CONSTRAINT iam_federation_flows_purpose_check CHECK (purpose IN ('login', 'link'))
);

CREATE TABLE iam_login_associations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    principal_id uuid NOT NULL,
    original_principal_id uuid NOT NULL,
    primary_login_id uuid NOT NULL,
    associated_login_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    CONSTRAINT iam_login_associations_check CHECK (((principal_id <> original_principal_id) AND (primary_login_id <> associated_login_id)))
);

CREATE TABLE iam_login_attempts (
    attempt_key text NOT NULL,
    failures integer DEFAULT 0 NOT NULL,
    locked_until timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE iam_login_merge_reviews (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id text NOT NULL,
    primary_login_id uuid NOT NULL,
    secondary_login_id uuid NOT NULL,
    primary_principal_id uuid NOT NULL,
    secondary_principal_id uuid NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone DEFAULT (now() + '00:30:00'::interval) NOT NULL,
    primary_device_id uuid NOT NULL,
    secondary_device_id uuid,
    association_id uuid,
    reason text NOT NULL,
    decision_reason text,
    CONSTRAINT iam_login_merge_reviews_check CHECK (((primary_login_id <> secondary_login_id) AND (primary_principal_id <> secondary_principal_id))),
    CONSTRAINT iam_login_merge_reviews_decision_reason_check CHECK (length(decision_reason) BETWEEN 5 AND 500),
    CONSTRAINT iam_login_merge_reviews_reason_check CHECK (length(reason) BETWEEN 5 AND 500),
    CONSTRAINT iam_login_merge_reviews_status_check CHECK (status IN ('pending', 'confirmed', 'applied', 'rejected', 'rolled_back'))
);

CREATE TABLE iam_login_sessions (
    token_hash text NOT NULL,
    login_account_id uuid NOT NULL,
    csrf_token text NOT NULL,
    device_label text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    external_identity_id uuid
);

CREATE TABLE iam_machine_credentials (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    issuer_id bigint NOT NULL,
    token_hash text NOT NULL,
    audience text NOT NULL,
    capabilities jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    CONSTRAINT iam_machine_credentials_audience_check CHECK (audience IN ('horizon-capability', 'horizon-agent')),
    CONSTRAINT iam_machine_credentials_capabilities_check CHECK (((jsonb_typeof(capabilities) = 'array'::text) AND ((jsonb_array_length(capabilities) >= 1) AND (jsonb_array_length(capabilities) <= 50)))),
    CONSTRAINT iam_machine_credentials_check CHECK ((expires_at > created_at))
);

CREATE TABLE iam_membership_invitations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id text NOT NULL,
    inviter_account_id bigint NOT NULL,
    username text NOT NULL,
    token_hash text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    accepted_at timestamp with time zone,
    recipient_realm text DEFAULT 'local'::text NOT NULL,
    CONSTRAINT iam_membership_invitations_recipient_realm_check CHECK ((recipient_realm ~ '^[a-z0-9][a-z0-9_-]{0,62}$'::text))
);

ALTER TABLE identity_organizations ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME identity_organizations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE iam_password_credentials (
    login_account_id uuid NOT NULL,
    password_hash text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE iam_pending_registrations (
    login_account_id uuid NOT NULL,
    tenant_id text NOT NULL,
    channel text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT iam_pending_registrations_channel_check CHECK (channel IN ('email', 'sms'))
);

CREATE TABLE iam_policy_heads (
    tenant_id text NOT NULL,
    version_id bigint NOT NULL
);

CREATE TABLE iam_policy_versions (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    author_id bigint NOT NULL,
    base_version bigint NOT NULL,
    rules jsonb NOT NULL,
    reason text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    published_at timestamp with time zone,
    reviewer_id bigint,
    author_principal_id uuid,
    reviewer_principal_id uuid,
    CONSTRAINT iam_policy_separate_principal CHECK ((author_principal_id <> reviewer_principal_id)),
    CONSTRAINT iam_policy_versions_base_version_check CHECK ((base_version >= 0)),
    CONSTRAINT iam_policy_versions_check CHECK (((reviewer_id IS NULL) = (published_at IS NULL))),
    CONSTRAINT iam_policy_versions_check1 CHECK ((reviewer_id <> author_id)),
    CONSTRAINT iam_policy_versions_reason_check CHECK (length(reason) BETWEEN 5 AND 500),
    CONSTRAINT iam_policy_versions_rules_check CHECK ((jsonb_typeof(rules) = 'array'::text))
);

ALTER TABLE iam_policy_versions ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME iam_policy_versions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE iam_reauthentication_proofs (
    token_hash text NOT NULL,
    tenant_id text NOT NULL,
    session_hash text NOT NULL,
    operation text NOT NULL,
    resource_id bigint NOT NULL,
    revision bigint NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    context_hash bytea NOT NULL
);

CREATE TABLE iam_recovery_codes (
    login_account_id uuid NOT NULL,
    code_hash text NOT NULL,
    used_at timestamp with time zone
);

CREATE TABLE iam_roles (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    name text NOT NULL,
    code text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    status text NOT NULL,
    grants jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    template_key text DEFAULT 'custom'::text NOT NULL,
    CONSTRAINT iam_roles_grants_check CHECK ((jsonb_typeof(grants) = 'array'::text)),
    CONSTRAINT iam_roles_name_check CHECK (char_length(name) BETWEEN 1 AND 100),
    CONSTRAINT iam_roles_status_check CHECK (status IN ('active', 'inactive')),
    CONSTRAINT iam_roles_template_key_check CHECK (template_key IN ('custom', 'tenant-admin'))
);

ALTER TABLE iam_roles ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME iam_roles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE iam_security_checkpoints (
    last_id bigint NOT NULL,
    previous_id bigint NOT NULL,
    previous_digest bytea NOT NULL,
    digest bytea NOT NULL,
    signature bytea NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    key_id text DEFAULT ''::text NOT NULL,
    CONSTRAINT iam_security_checkpoints_check CHECK (((previous_id >= 0) AND (previous_id < last_id))),
    CONSTRAINT iam_security_checkpoints_digest_check CHECK ((octet_length(digest) = 32)),
    CONSTRAINT iam_security_checkpoints_key_format CHECK (((key_id = ''::text) OR (key_id ~ '^[a-f0-9]{64}$'::text))),
    CONSTRAINT iam_security_checkpoints_previous_digest_check CHECK ((octet_length(previous_digest) = 32)),
    CONSTRAINT iam_security_checkpoints_signature_check CHECK ((octet_length(signature) = 64))
);

CREATE TABLE iam_security_event_archive (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    account_id bigint DEFAULT 0 NOT NULL,
    binding_id uuid,
    action text NOT NULL,
    outcome text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    correlation_id text DEFAULT ''::text NOT NULL,
    resource_type text DEFAULT ''::text NOT NULL,
    resource_id text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    authority jsonb,
    CONSTRAINT iam_security_event_archive_authority_check CHECK ((jsonb_typeof(authority) = 'object'::text))
);

CREATE TABLE iam_security_events (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    account_id bigint DEFAULT 0 NOT NULL,
    binding_id uuid,
    action text NOT NULL,
    outcome text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    correlation_id text DEFAULT ''::text NOT NULL,
    resource_type text DEFAULT ''::text NOT NULL,
    resource_id text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    authority jsonb,
    CONSTRAINT iam_security_events_authority_check CHECK ((jsonb_typeof(authority) = 'object'::text))
);

CREATE VIEW iam_security_event_history AS
 SELECT iam_security_events.id,
    iam_security_events.tenant_id,
    iam_security_events.account_id,
    iam_security_events.binding_id,
    iam_security_events.action,
    iam_security_events.outcome,
    iam_security_events.reason,
    iam_security_events.correlation_id,
    iam_security_events.resource_type,
    iam_security_events.resource_id,
    iam_security_events.created_at,
    iam_security_events.authority
   FROM iam_security_events
UNION ALL
 SELECT iam_security_event_archive.id,
    iam_security_event_archive.tenant_id,
    iam_security_event_archive.account_id,
    iam_security_event_archive.binding_id,
    iam_security_event_archive.action,
    iam_security_event_archive.outcome,
    iam_security_event_archive.reason,
    iam_security_event_archive.correlation_id,
    iam_security_event_archive.resource_type,
    iam_security_event_archive.resource_id,
    iam_security_event_archive.created_at,
    iam_security_event_archive.authority
   FROM iam_security_event_archive;

ALTER TABLE iam_security_events ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME iam_security_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE iam_security_retention_receipts (
    checkpoint_id bigint NOT NULL,
    cutoff timestamp with time zone NOT NULL,
    signature bytea NOT NULL,
    key_id text NOT NULL,
    CONSTRAINT iam_security_retention_receipts_key_id_check CHECK ((key_id <> ''::text)),
    CONSTRAINT iam_security_retention_receipts_signature_check CHECK ((octet_length(signature) = 64))
);

CREATE TABLE iam_sessions (
    token_hash text NOT NULL,
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    csrf_token text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    identity_binding_id uuid,
    public_id uuid DEFAULT gen_random_uuid() NOT NULL,
    device_label text DEFAULT ''::text NOT NULL,
    external_identity_id uuid,
    refreshed_at timestamp with time zone,
    CONSTRAINT iam_sessions_device_label_check CHECK ((char_length(device_label) <= 160))
);

CREATE TABLE iam_temporary_requests (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    applicant_id bigint NOT NULL,
    requested_grants jsonb NOT NULL,
    requested_minutes integer NOT NULL,
    reason text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    approved_grants jsonb DEFAULT '[]'::jsonb NOT NULL,
    resolved_scopes jsonb DEFAULT '{}'::jsonb NOT NULL,
    approved_minutes integer DEFAULT 0 NOT NULL,
    reviewer_id bigint,
    review_note text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    reviewed_at timestamp with time zone,
    expires_at timestamp with time zone,
    cancelled_at timestamp with time zone,
    revoked_at timestamp with time zone,
    revoked_by bigint,
    CONSTRAINT iam_temporary_requests_check CHECK (((reviewer_id IS NULL) OR (reviewer_id <> applicant_id))),
    CONSTRAINT iam_temporary_requests_check1 CHECK (((status <> ALL (ARRAY['approved'::text, 'revoked'::text])) OR ((reviewer_id IS NOT NULL) AND (reviewed_at IS NOT NULL) AND (expires_at > reviewed_at) AND (approved_minutes = ANY (ARRAY[30, 60, 120, 240, 480, 1440, 4320, 10080]))))),
    CONSTRAINT iam_temporary_requests_reason_check CHECK (char_length(reason) BETWEEN 5 AND 500),
    CONSTRAINT iam_temporary_requests_requested_grants_check CHECK ((jsonb_typeof(requested_grants) = 'array'::text)),
    CONSTRAINT iam_temporary_requests_requested_minutes_check CHECK (requested_minutes IN (30, 60, 120, 240, 480, 1440, 4320, 10080)),
    CONSTRAINT iam_temporary_requests_status_check CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled', 'revoked'))
);

ALTER TABLE iam_temporary_requests ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME iam_temporary_requests_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE VIEW iam_tenant_account_authority AS
 SELECT subject.tenant_id,
    subject.account_id,
    authority.is_admin
   FROM (iam_active_account_subjects subject
     CROSS JOIN LATERAL ( SELECT (EXISTS ( SELECT 1
                   FROM (iam_account_roles assignment
                     JOIN iam_roles role ON (((role.tenant_id = assignment.tenant_id) AND (role.id = assignment.role_id))))
                  WHERE ((assignment.tenant_id = subject.tenant_id) AND (assignment.account_id = subject.account_id) AND (role.template_key = 'tenant-admin'::text) AND (role.status = 'active'::text)))) AS is_admin) authority)
  WHERE (authority.is_admin OR (EXISTS ( SELECT 1
           FROM (iam_context_role_grants assignment
             JOIN iam_roles role ON (((role.tenant_id = assignment.tenant_id) AND (role.id = assignment.role_id))))
          WHERE ((assignment.tenant_id = subject.tenant_id) AND (assignment.principal_id = subject.principal_id) AND (assignment.identity_binding_id IS NULL) AND (assignment.revoked_at IS NULL) AND (assignment.expires_at > now()) AND (role.template_key = 'custom'::text) AND (role.status = 'active'::text)))));

CREATE TABLE iam_totp_credentials (
    login_account_id uuid NOT NULL,
    sealed_secret text NOT NULL,
    enrollment_context_hash text NOT NULL,
    activated_at timestamp with time zone,
    enroll_expires_at timestamp with time zone NOT NULL,
    last_counter bigint DEFAULT '-1'::integer NOT NULL,
    replacement_secret text,
    replacement_context_hash text,
    replacement_expires_at timestamp with time zone
);

CREATE TABLE iam_verified_contacts (
    login_account_id uuid NOT NULL,
    channel text NOT NULL,
    destination text NOT NULL,
    verified_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT iam_verified_contacts_channel_check CHECK (channel IN ('email', 'sms'))
);

ALTER TABLE identity_account_profiles ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME identity_account_profiles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE kb_chunks (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    document_id bigint NOT NULL,
    ordinal integer NOT NULL,
    content text NOT NULL,
    CONSTRAINT kb_chunks_content_check CHECK (length(content) BETWEEN 1 AND 4000),
    CONSTRAINT kb_chunks_ordinal_check CHECK ((ordinal >= 0))
);

ALTER TABLE kb_chunks ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME kb_chunks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE kb_documents (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    title text NOT NULL,
    source text DEFAULT 'manual'::text NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    chunks integer DEFAULT 0 NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    version text DEFAULT ''::text NOT NULL,
    CONSTRAINT kb_documents_chunks_check CHECK ((chunks >= 0)),
    CONSTRAINT kb_documents_source_check CHECK (source IN ('manual', 'file')),
    CONSTRAINT kb_documents_title_check CHECK (length(title) BETWEEN 1 AND 200)
);

ALTER TABLE kb_documents ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME kb_documents_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE mail_channels (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    mailbox_id bigint NOT NULL,
    direction text NOT NULL,
    protocol text NOT NULL,
    host text NOT NULL,
    port integer NOT NULL,
    tls_mode text NOT NULL,
    username text NOT NULL,
    credential_id bigint NOT NULL,
    verification_status text DEFAULT 'unknown'::text NOT NULL,
    last_error_code text DEFAULT ''::text NOT NULL,
    last_verified_at timestamp with time zone,
    next_sync_at timestamp with time zone,
    imap_uid_validity bigint DEFAULT 0 NOT NULL,
    imap_last_uid bigint DEFAULT 0 NOT NULL,
    lease_until timestamp with time zone,
    lease_owner text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT mail_channels_check CHECK ((direction = 'send' AND protocol = 'smtp') OR (direction = 'receive' AND (protocol = 'imap' OR (protocol = 'pop3' AND tls_mode = 'tls')))),
    CONSTRAINT mail_channels_direction_check CHECK (direction IN ('send', 'receive')),
    CONSTRAINT mail_channels_host_check CHECK (length(host) BETWEEN 1 AND 253),
    CONSTRAINT mail_channels_imap_uid_validity_check CHECK (imap_uid_validity BETWEEN 0 AND 4294967295),
    CONSTRAINT mail_channels_imap_last_uid_check CHECK (imap_last_uid BETWEEN 0 AND 4294967295),
    CONSTRAINT mail_channels_port_check CHECK (((port >= 1) AND (port <= 65535))),
    CONSTRAINT mail_channels_protocol_check CHECK (protocol IN ('smtp', 'pop3', 'imap')),
    CONSTRAINT mail_channels_tls_mode_check CHECK (tls_mode IN ('starttls', 'tls')),
    CONSTRAINT mail_channels_username_check CHECK (length(username) BETWEEN 0 AND 200),
    CONSTRAINT mail_channels_verification_status_check CHECK (verification_status IN ('unknown', 'ok', 'failed'))
);

ALTER TABLE mail_channels ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME mail_channels_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE mail_credentials (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    mailbox_id bigint NOT NULL,
    purpose text NOT NULL,
    ciphertext text NOT NULL,
    fingerprint text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    rotated_at timestamp with time zone DEFAULT now() NOT NULL,
    last_used_at timestamp with time zone,
    CONSTRAINT mail_credentials_ciphertext_check CHECK (length(ciphertext) BETWEEN 12 AND 20000),
    CONSTRAINT mail_credentials_fingerprint_check CHECK ((length(fingerprint) = 16)),
    CONSTRAINT mail_credentials_purpose_check CHECK (purpose IN ('smtp', 'pop3')),
    CONSTRAINT mail_credentials_version_check CHECK ((version > 0))
);

ALTER TABLE mail_credentials ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME mail_credentials_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE mail_inbox_syncs (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    mailbox_id bigint NOT NULL,
    status text DEFAULT 'running'::text NOT NULL,
    fetched_count integer DEFAULT 0 NOT NULL,
    imported_count integer DEFAULT 0 NOT NULL,
    duplicate_count integer DEFAULT 0 NOT NULL,
    error_code text DEFAULT ''::text NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    CONSTRAINT mail_inbox_syncs_duplicate_count_check CHECK ((duplicate_count >= 0)),
    CONSTRAINT mail_inbox_syncs_fetched_count_check CHECK ((fetched_count >= 0)),
    CONSTRAINT mail_inbox_syncs_imported_count_check CHECK ((imported_count >= 0)),
    CONSTRAINT mail_inbox_syncs_status_check CHECK (status IN ('running', 'completed', 'failed'))
);

ALTER TABLE mail_inbox_syncs ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME mail_inbox_syncs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE mail_mailboxes (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    owner_account_id bigint NOT NULL,
    name text NOT NULL,
    email_address text NOT NULL,
    default_send boolean DEFAULT false NOT NULL,
    status text DEFAULT 'unverified'::text NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT mail_mailboxes_email_address_check CHECK (length(email_address) BETWEEN 3 AND 254),
    CONSTRAINT mail_mailboxes_name_check CHECK (length(name) BETWEEN 1 AND 120),
    CONSTRAINT mail_mailboxes_revision_check CHECK ((revision > 0)),
    CONSTRAINT mail_mailboxes_status_check CHECK (status IN ('unverified', 'active', 'disabled'))
);

ALTER TABLE mail_mailboxes ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME mail_mailboxes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE mail_message_attachments (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    message_id bigint NOT NULL,
    file_id bigint NOT NULL,
    content_id text DEFAULT ''::text NOT NULL,
    filename text NOT NULL,
    mime_type text NOT NULL,
    size bigint NOT NULL,
    disposition text DEFAULT 'attachment'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT mail_message_attachments_content_id_check CHECK ((length(content_id) <= 255)),
    CONSTRAINT mail_message_attachments_disposition_check CHECK (disposition IN ('attachment', 'inline')),
    CONSTRAINT mail_message_attachments_filename_check CHECK (length(filename) BETWEEN 1 AND 255),
    CONSTRAINT mail_message_attachments_mime_type_check CHECK (length(mime_type) BETWEEN 3 AND 255),
    CONSTRAINT mail_message_attachments_size_check CHECK ((size > 0))
);

ALTER TABLE mail_message_attachments ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME mail_message_attachments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE mail_message_recipients (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    message_id bigint NOT NULL,
    kind text NOT NULL,
    address text NOT NULL,
    display_name text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT mail_message_recipients_address_check CHECK (length(address) BETWEEN 3 AND 320),
    CONSTRAINT mail_message_recipients_display_name_check CHECK ((length(display_name) <= 200)),
    CONSTRAINT mail_message_recipients_kind_check CHECK (kind IN ('to', 'cc', 'bcc'))
);

ALTER TABLE mail_message_recipients ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME mail_message_recipients_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE mail_messages (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    mailbox_id bigint NOT NULL,
    owner_account_id bigint NOT NULL,
    direction text NOT NULL,
    folder text NOT NULL,
    provider_uid text NOT NULL,
    rfc_message_id text DEFAULT ''::text NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    from_address text DEFAULT ''::text NOT NULL,
    reply_to text DEFAULT ''::text NOT NULL,
    body_text text NOT NULL,
    body_html text DEFAULT ''::text NOT NULL,
    snippet text DEFAULT ''::text NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL,
    read_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT mail_messages_body_html_check CHECK ((length(body_html) <= 26214400)),
    CONSTRAINT mail_messages_body_text_check CHECK ((length(body_text) <= 26214400)),
    CONSTRAINT mail_messages_direction_check CHECK (direction IN ('inbound', 'outbound')),
    CONSTRAINT mail_messages_folder_check CHECK (folder IN ('inbox', 'sent')),
    CONSTRAINT mail_messages_from_address_check CHECK ((length(from_address) <= 320)),
    CONSTRAINT mail_messages_provider_uid_check CHECK (length(provider_uid) BETWEEN 1 AND 255),
    CONSTRAINT mail_messages_reply_to_check CHECK ((length(reply_to) <= 320)),
    CONSTRAINT mail_messages_rfc_message_id_check CHECK ((length(rfc_message_id) <= 998)),
    CONSTRAINT mail_messages_snippet_check CHECK (length(snippet) BETWEEN 0 AND 500),
    CONSTRAINT mail_messages_subject_check CHECK ((length(subject) <= 998))
);

ALTER TABLE mail_messages ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME mail_messages_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE mail_outbound_deliveries (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    mailbox_id bigint NOT NULL,
    message_id bigint NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    lease_until timestamp with time zone,
    lease_owner text DEFAULT ''::text NOT NULL,
    error_code text DEFAULT ''::text NOT NULL,
    submitted_by bigint NOT NULL,
    sent_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT mail_outbound_deliveries_attempts_check CHECK ((attempts >= 0)),
    CONSTRAINT mail_outbound_deliveries_status_check CHECK (status IN ('queued', 'sending', 'sent', 'failed', 'ambiguous', 'cancelled'))
);

ALTER TABLE mail_outbound_deliveries ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME mail_outbound_deliveries_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE mcp_call_audits (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    token_id bigint NOT NULL,
    tool text NOT NULL,
    result_code text NOT NULL,
    duration_ms bigint NOT NULL,
    request_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT mcp_call_audits_duration_ms_check CHECK ((duration_ms >= 0))
);

ALTER TABLE mcp_call_audits ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME mcp_call_audits_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE mcp_token_tools (
    token_id bigint NOT NULL,
    tool text NOT NULL
);

CREATE TABLE mcp_tokens (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    account_id bigint NOT NULL,
    name text NOT NULL,
    token_hash text NOT NULL,
    token_prefix text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    last_used_at timestamp with time zone,
    revoked_by bigint,
    identity_binding_id uuid,
    CONSTRAINT mcp_tokens_check CHECK (((expires_at > created_at) AND (expires_at <= (created_at + '90 days'::interval)))),
    CONSTRAINT mcp_tokens_name_check CHECK (length(name) BETWEEN 1 AND 100),
    CONSTRAINT mcp_tokens_token_hash_check CHECK ((length(token_hash) = 64))
);

ALTER TABLE mcp_tokens ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME mcp_tokens_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE memory_access_audit (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    actor_id bigint NOT NULL,
    action text NOT NULL,
    object_id bigint DEFAULT 0 NOT NULL,
    correlation_id text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE memory_access_audit ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME memory_access_audit_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE memory_corrections (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    memory_id bigint NOT NULL,
    actor_id bigint NOT NULL,
    previous_version bigint NOT NULL,
    previous_title text NOT NULL,
    previous_summary text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE memory_corrections ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME memory_corrections_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE memory_event_inbox (
    source_event_id bigint NOT NULL,
    tenant_id text NOT NULL,
    event_id text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    available_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    dead_at timestamp with time zone,
    error_code text DEFAULT ''::text NOT NULL,
    CONSTRAINT memory_event_inbox_attempts_check CHECK ((attempts >= 0))
);

CREATE TABLE memory_events (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    organization_id text DEFAULT ''::text NOT NULL,
    event_id text NOT NULL,
    event_type text NOT NULL,
    event_version text NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    ingested_at timestamp with time zone DEFAULT now() NOT NULL,
    actor_id text NOT NULL,
    actor_type text NOT NULL,
    department_id bigint DEFAULT 0 NOT NULL,
    source_type text NOT NULL,
    source_id text DEFAULT ''::text NOT NULL,
    source_module text DEFAULT ''::text NOT NULL,
    resource_type text DEFAULT ''::text NOT NULL,
    resource_id text DEFAULT ''::text NOT NULL,
    action_name text DEFAULT ''::text NOT NULL,
    result text DEFAULT ''::text NOT NULL,
    invocation_id text DEFAULT ''::text NOT NULL,
    session_id text DEFAULT ''::text NOT NULL,
    task_id text DEFAULT ''::text NOT NULL,
    workflow_instance_id text DEFAULT ''::text NOT NULL,
    trace_id text DEFAULT ''::text NOT NULL,
    correlation_id text DEFAULT ''::text NOT NULL,
    causation_id text DEFAULT ''::text NOT NULL,
    trigger_depth integer DEFAULT 0 NOT NULL,
    sensitivity text DEFAULT 'internal'::text NOT NULL,
    candidate_partitions text[] DEFAULT '{}'::text[] NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT memory_events_trigger_depth_check CHECK ((trigger_depth >= 0))
);

ALTER TABLE memory_events ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME memory_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE memory_items (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    partition text NOT NULL,
    memory_type text NOT NULL,
    scope_type text NOT NULL,
    scope_id text NOT NULL,
    owner_id bigint NOT NULL,
    title text NOT NULL,
    summary text NOT NULL,
    sensitivity text DEFAULT 'internal'::text NOT NULL,
    verification_status text DEFAULT 'unverified'::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    valid_from timestamp with time zone DEFAULT now() NOT NULL,
    valid_to timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT memory_items_check CHECK (((valid_to IS NULL) OR (valid_to >= valid_from))),
    CONSTRAINT memory_items_partition_check CHECK (partition IN ('agent', 'activity', 'exception', 'business', 'knowledge', 'execution')),
    CONSTRAINT memory_items_scope_type_check CHECK (scope_type IN ('user', 'project', 'department', 'organization', 'business_entity')),
    CONSTRAINT memory_items_status_check CHECK (status IN ('active', 'archived', 'deleted')),
    CONSTRAINT memory_items_verification_status_check CHECK (verification_status IN ('unverified', 'confirmed', 'rejected')),
    CONSTRAINT memory_items_version_check CHECK ((version > 0))
);

ALTER TABLE memory_items ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME memory_items_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE memory_partition_policies (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    partition text NOT NULL,
    retention_days integer DEFAULT 0 NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    updated_by bigint NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT memory_partition_policies_partition_check CHECK (partition IN ('agent', 'activity', 'exception', 'business', 'knowledge', 'execution')),
    CONSTRAINT memory_partition_policies_retention_days_check CHECK ((retention_days >= 0)),
    CONSTRAINT memory_partition_policies_revision_check CHECK ((revision > 0))
);

ALTER TABLE memory_partition_policies ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME memory_partition_policies_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE memory_relations (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    source_memory_id bigint NOT NULL,
    target_memory_id bigint NOT NULL,
    relation_type text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT memory_relations_check CHECK ((source_memory_id <> target_memory_id))
);

ALTER TABLE memory_relations ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME memory_relations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE memory_sources (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    memory_id bigint NOT NULL,
    event_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE memory_sources ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME memory_sources_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE orch_checkpoints (
    tenant_id text NOT NULL,
    run_id bigint NOT NULL,
    checkpoint_key text NOT NULL,
    payload text NOT NULL,
    lease_until timestamp with time zone DEFAULT '-infinity'::timestamp with time zone NOT NULL,
    lease_token text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT orch_checkpoints_payload_check CHECK ((length(payload) <= 8388608))
);

CREATE TABLE project_members (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    project_id bigint NOT NULL,
    account_id bigint NOT NULL,
    joined_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE project_members ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME project_members_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE project_plans (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    project_id bigint NOT NULL,
    plan_type text DEFAULT 'phase'::text NOT NULL,
    title text NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    priority text DEFAULT 'normal'::text NOT NULL,
    due_date date,
    owner_id bigint NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT project_plans_content_check CHECK (length(content) BETWEEN 0 AND 20000),
    CONSTRAINT project_plans_plan_type_check CHECK (plan_type IN ('milestone', 'phase', 'deliverable')),
    CONSTRAINT project_plans_priority_check CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    CONSTRAINT project_plans_status_check CHECK (status IN ('pending', 'in_progress', 'done', 'cancelled')),
    CONSTRAINT project_plans_title_check CHECK (length(title) BETWEEN 1 AND 200)
);

ALTER TABLE project_plans ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME project_plans_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE project_task_briefs (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    task_id bigint NOT NULL,
    run_id bigint DEFAULT 0 NOT NULL,
    summary text NOT NULL,
    model text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT project_task_briefs_summary_check CHECK (length(summary) BETWEEN 1 AND 4000)
);

ALTER TABLE project_task_briefs ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME project_task_briefs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE project_tasks (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    project_id bigint NOT NULL,
    plan_id bigint,
    title text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'todo'::text NOT NULL,
    priority text DEFAULT 'normal'::text NOT NULL,
    assignee_id bigint,
    due_date date,
    completed_at timestamp with time zone,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT project_tasks_description_check CHECK (length(description) BETWEEN 0 AND 20000),
    CONSTRAINT project_tasks_priority_check CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    CONSTRAINT project_tasks_status_check CHECK (status IN ('todo', 'in_progress', 'done', 'cancelled')),
    CONSTRAINT project_tasks_title_check CHECK (length(title) BETWEEN 1 AND 200)
);

ALTER TABLE project_tasks ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME project_tasks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE projects (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    code text NOT NULL,
    name text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    priority text DEFAULT 'normal'::text NOT NULL,
    due_date date,
    owner_id bigint NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT projects_code_check CHECK (length(code) BETWEEN 1 AND 32),
    CONSTRAINT projects_name_check CHECK (length(name) BETWEEN 1 AND 120),
    CONSTRAINT projects_priority_check CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    CONSTRAINT projects_status_check CHECK (status IN ('active', 'paused', 'completed', 'archived'))
);

ALTER TABLE projects ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME projects_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE runtime_application_changes (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    application_id text NOT NULL,
    action text NOT NULL,
    version text DEFAULT ''::text NOT NULL,
    digest text DEFAULT ''::text NOT NULL,
    diagnostic_code text DEFAULT ''::text NOT NULL,
    account_id bigint NOT NULL,
    correlation_id text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE runtime_application_changes ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME runtime_application_changes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE runtime_application_heads (
    tenant_id text NOT NULL,
    application_id text NOT NULL,
    plugin_id text NOT NULL,
    published_version text DEFAULT ''::text NOT NULL,
    proposal_version text DEFAULT ''::text NOT NULL,
    route_root text DEFAULT ''::text NOT NULL,
    base_digest text NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    next_configuration bigint DEFAULT 1 NOT NULL,
    compatibility text DEFAULT 'compatible'::text NOT NULL,
    CONSTRAINT runtime_application_heads_compatibility_check CHECK (compatibility IN ('compatible', 'incompatible', 'pending_rebase'))
);

CREATE TABLE runtime_application_states (
    tenant_id text NOT NULL,
    application_id text NOT NULL,
    plugin_id text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    revision bigint DEFAULT 1 NOT NULL
);

CREATE TABLE runtime_application_versions (
    tenant_id text NOT NULL,
    application_id text NOT NULL,
    version text NOT NULL,
    parent_version text DEFAULT ''::text NOT NULL,
    base_digest text NOT NULL,
    definition jsonb NOT NULL,
    digest text NOT NULL,
    source text NOT NULL,
    created_by bigint NOT NULL,
    agent_run_id bigint,
    correlation_id text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT runtime_application_versions_definition_check CHECK ((jsonb_typeof(definition) = 'object'::text)),
    CONSTRAINT runtime_application_versions_definition_check1 CHECK ((octet_length((definition)::text) <= 1048576)),
    CONSTRAINT runtime_application_versions_source_check CHECK (source IN ('deployment', 'human', 'agent', 'rollback', 'rebase')),
    CONSTRAINT runtime_application_versions_version_check CHECK ((version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'::text))
);

CREATE TABLE runtime_deployment_snapshots (
    digest text NOT NULL,
    plugin_id text NOT NULL,
    plugin_version text NOT NULL,
    application_id text NOT NULL,
    base_version text NOT NULL,
    definition jsonb NOT NULL,
    contracts_digest text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT runtime_deployment_snapshots_definition_check CHECK ((jsonb_typeof(definition) = 'object'::text)),
    CONSTRAINT runtime_deployment_snapshots_definition_check1 CHECK ((octet_length((definition)::text) <= 1048576))
);

CREATE TABLE runtime_installation_history (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    plugin_id text NOT NULL,
    account_id bigint NOT NULL,
    action text NOT NULL,
    version text NOT NULL,
    revision bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE runtime_installation_history ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME runtime_installation_history_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE runtime_installation_templates (
    plugin_id text NOT NULL,
    version text NOT NULL,
    application_id text NOT NULL,
    enabled boolean DEFAULT false NOT NULL
);

CREATE TABLE runtime_installations (
    tenant_id text NOT NULL,
    plugin_id text NOT NULL,
    version text NOT NULL,
    status text NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    digest text DEFAULT ''::text NOT NULL,
    previous_version text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT runtime_installations_status_check CHECK (status IN ('installed', 'enabled', 'disabled', 'uninstalled'))
);

CREATE TABLE runtime_invocation_dependencies (
    tenant_id text NOT NULL,
    invocation_id text NOT NULL,
    plugin_id text NOT NULL
);

CREATE TABLE runtime_run_dependencies (
    tenant_id text NOT NULL,
    run_id bigint NOT NULL,
    plugin_id text NOT NULL
);

CREATE TABLE technical_demo_records (
    id bigint NOT NULL,
    tenant_id text NOT NULL,
    organization_id text NOT NULL,
    department_id text NOT NULL,
    owner_id text NOT NULL,
    label text NOT NULL,
    internal_note text DEFAULT ''::text NOT NULL,
    CONSTRAINT technical_demo_records_label_check CHECK (char_length(label) BETWEEN 1 AND 80)
);

ALTER TABLE technical_demo_records ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME technical_demo_records_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

ALTER TABLE ai_configuration
    ADD CONSTRAINT ai_configuration_pkey PRIMARY KEY (tenant_id);

ALTER TABLE ai_conversations
    ADD CONSTRAINT ai_conversations_pkey PRIMARY KEY (id);

ALTER TABLE ai_conversations
    ADD CONSTRAINT ai_conversations_tenant_id_account_id_session_id_key UNIQUE (tenant_id, account_id, session_id);

ALTER TABLE ai_conversations
    ADD CONSTRAINT ai_conversations_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE ai_run_events
    ADD CONSTRAINT ai_run_events_pkey PRIMARY KEY (tenant_id, run_id, sequence);

ALTER TABLE ai_runs
    ADD CONSTRAINT ai_runs_pkey PRIMARY KEY (id);

ALTER TABLE ai_runs
    ADD CONSTRAINT ai_runs_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE ai_trigger_executions
    ADD CONSTRAINT ai_trigger_executions_pkey PRIMARY KEY (id);

ALTER TABLE ai_trigger_executions
    ADD CONSTRAINT ai_trigger_executions_tenant_id_policy_id_event_id_key UNIQUE (tenant_id, policy_id, event_id);

ALTER TABLE ai_trigger_policies
    ADD CONSTRAINT ai_trigger_policies_pkey PRIMARY KEY (id);

ALTER TABLE ai_trigger_policies
    ADD CONSTRAINT ai_trigger_policies_tenant_id_policy_id_version_key UNIQUE (tenant_id, policy_id, version);

ALTER TABLE articles
    ADD CONSTRAINT articles_pkey PRIMARY KEY (id);

ALTER TABLE articles
    ADD CONSTRAINT articles_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE automation_runs
    ADD CONSTRAINT automation_runs_pkey PRIMARY KEY (id);

ALTER TABLE automation_runs
    ADD CONSTRAINT automation_runs_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE automation_runs
    ADD CONSTRAINT automation_runs_tenant_id_task_id_occurrence_key_key UNIQUE (tenant_id, task_id, occurrence_key);

ALTER TABLE automation_tasks
    ADD CONSTRAINT automation_tasks_pkey PRIMARY KEY (id);

ALTER TABLE automation_tasks
    ADD CONSTRAINT automation_tasks_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE cap_approvals
    ADD CONSTRAINT cap_approvals_pkey PRIMARY KEY (id);

ALTER TABLE cap_approvals
    ADD CONSTRAINT cap_approvals_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE cap_events
    ADD CONSTRAINT cap_events_pkey PRIMARY KEY (id);

ALTER TABLE cap_events
    ADD CONSTRAINT cap_events_tenant_id_event_id_key UNIQUE (tenant_id, event_id);

ALTER TABLE cap_execution_binding_changes
    ADD CONSTRAINT cap_execution_binding_changes_pkey PRIMARY KEY (id);

ALTER TABLE cap_execution_bindings
    ADD CONSTRAINT cap_execution_bindings_pkey PRIMARY KEY (tenant_id, capability, capability_version);

ALTER TABLE cap_execution_plans
    ADD CONSTRAINT cap_execution_plans_pkey PRIMARY KEY (tenant_id, plan_id, version);

ALTER TABLE cap_execution_plans
    ADD CONSTRAINT cap_execution_plans_tenant_id_capability_capability_version_key UNIQUE (tenant_id, capability, capability_version, plan_id, version);

ALTER TABLE cap_execution_records
    ADD CONSTRAINT cap_execution_records_pkey PRIMARY KEY (tenant_id, invocation_id);

ALTER TABLE cap_invocations
    ADD CONSTRAINT cap_invocations_pkey PRIMARY KEY (id);

ALTER TABLE cap_invocations
    ADD CONSTRAINT cap_invocations_tenant_id_invocation_id_key UNIQUE (tenant_id, invocation_id);

ALTER TABLE cap_outbox
    ADD CONSTRAINT cap_outbox_pkey PRIMARY KEY (id);

ALTER TABLE cap_outbox
    ADD CONSTRAINT cap_outbox_tenant_id_event_id_key UNIQUE (tenant_id, event_id);

ALTER TABLE drive_files
    ADD CONSTRAINT drive_files_pkey PRIMARY KEY (id);

ALTER TABLE drive_files
    ADD CONSTRAINT drive_files_tenant_id_file_id_key UNIQUE (tenant_id, file_id);

ALTER TABLE drive_files
    ADD CONSTRAINT drive_files_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE drive_folders
    ADD CONSTRAINT drive_folders_pkey PRIMARY KEY (id);

ALTER TABLE drive_folders
    ADD CONSTRAINT drive_folders_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE drive_folders
    ADD CONSTRAINT drive_folders_tenant_id_space_id_id_key UNIQUE (tenant_id, space_id, id);

ALTER TABLE drive_members
    ADD CONSTRAINT drive_members_pkey PRIMARY KEY (id);

ALTER TABLE drive_members
    ADD CONSTRAINT drive_members_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE drive_spaces
    ADD CONSTRAINT drive_spaces_pkey PRIMARY KEY (id);

ALTER TABLE drive_spaces
    ADD CONSTRAINT drive_spaces_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE drive_trash_batches
    ADD CONSTRAINT drive_trash_batches_pkey PRIMARY KEY (id);

ALTER TABLE drive_trash_batches
    ADD CONSTRAINT drive_trash_batches_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE drive_trash_batches
    ADD CONSTRAINT drive_trash_batches_tenant_id_space_id_id_key UNIQUE (tenant_id, space_id, id);

ALTER TABLE drive_upload_chunks
    ADD CONSTRAINT drive_upload_chunks_pkey PRIMARY KEY (id);

ALTER TABLE drive_upload_chunks
    ADD CONSTRAINT drive_upload_chunks_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE drive_upload_chunks
    ADD CONSTRAINT drive_upload_chunks_tenant_id_upload_id_chunk_index_key UNIQUE (tenant_id, upload_id, chunk_index);

ALTER TABLE drive_upload_chunks
    ADD CONSTRAINT drive_upload_chunks_tenant_id_upload_id_storage_key_key UNIQUE (tenant_id, upload_id, storage_key);

ALTER TABLE drive_uploads
    ADD CONSTRAINT drive_uploads_pkey PRIMARY KEY (id);

ALTER TABLE drive_uploads
    ADD CONSTRAINT drive_uploads_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE drive_uploads
    ADD CONSTRAINT drive_uploads_tenant_id_space_id_id_key UNIQUE (tenant_id, space_id, id);

ALTER TABLE drive_uploads
    ADD CONSTRAINT drive_uploads_tenant_id_storage_profile_id_object_key_key UNIQUE (tenant_id, storage_profile_id, object_key);

ALTER TABLE example_orders
    ADD CONSTRAINT example_orders_pkey PRIMARY KEY (id);

ALTER TABLE example_records
    ADD CONSTRAINT example_records_pkey PRIMARY KEY (id);

ALTER TABLE example_records
    ADD CONSTRAINT example_records_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE gov_announcement_files
    ADD CONSTRAINT gov_announcement_files_pkey PRIMARY KEY (tenant_id, announcement_id, file_id);

ALTER TABLE gov_announcements
    ADD CONSTRAINT gov_announcements_pkey PRIMARY KEY (id);

ALTER TABLE gov_announcements
    ADD CONSTRAINT gov_announcements_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE gov_announcements
    ADD CONSTRAINT gov_announcements_tenant_id_sender_id_client_key_key UNIQUE (tenant_id, sender_id, client_key);

ALTER TABLE gov_audit
    ADD CONSTRAINT gov_audit_pkey PRIMARY KEY (id);

ALTER TABLE gov_deliveries
    ADD CONSTRAINT gov_deliveries_pkey PRIMARY KEY (id);

ALTER TABLE gov_deliveries
    ADD CONSTRAINT gov_deliveries_tenant_id_message_id_channel_key UNIQUE (tenant_id, message_id, channel);

ALTER TABLE gov_dictionaries
    ADD CONSTRAINT gov_dictionaries_pkey PRIMARY KEY (id);

ALTER TABLE gov_dictionaries
    ADD CONSTRAINT gov_dictionaries_tenant_id_code_key UNIQUE (tenant_id, code);

ALTER TABLE gov_dictionaries
    ADD CONSTRAINT gov_dictionaries_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE gov_dictionary_items
    ADD CONSTRAINT gov_dictionary_items_pkey PRIMARY KEY (id);

ALTER TABLE gov_dictionary_items
    ADD CONSTRAINT gov_dictionary_items_tenant_id_dictionary_id_code_key UNIQUE (tenant_id, dictionary_id, code);

ALTER TABLE gov_dictionary_items
    ADD CONSTRAINT gov_dictionary_items_tenant_id_dictionary_id_id_key UNIQUE (tenant_id, dictionary_id, id);

ALTER TABLE gov_dictionary_items
    ADD CONSTRAINT gov_dictionary_items_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE gov_dictionary_overrides
    ADD CONSTRAINT gov_dictionary_overrides_pkey PRIMARY KEY (tenant_id, dictionary_id, item_id, scope_key);

ALTER TABLE gov_dictionary_references
    ADD CONSTRAINT gov_dictionary_references_pkey PRIMARY KEY (tenant_id, dictionary_id, item_id, resource_type, resource_id);

ALTER TABLE gov_drafts
    ADD CONSTRAINT gov_drafts_pkey PRIMARY KEY (id);

ALTER TABLE gov_drafts
    ADD CONSTRAINT gov_drafts_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE gov_events
    ADD CONSTRAINT gov_events_pkey PRIMARY KEY (id);

ALTER TABLE gov_events
    ADD CONSTRAINT gov_events_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE gov_events
    ADD CONSTRAINT gov_events_tenant_id_request_id_event_key UNIQUE (tenant_id, request_id, event);

ALTER TABLE gov_file_bindings
    ADD CONSTRAINT gov_file_bindings_pkey PRIMARY KEY (tenant_id, file_id, scope_key, revision);

ALTER TABLE gov_files
    ADD CONSTRAINT gov_files_pkey PRIMARY KEY (id);

ALTER TABLE gov_files
    ADD CONSTRAINT gov_files_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE gov_files
    ADD CONSTRAINT gov_files_tenant_id_storage_profile_id_storage_key_key UNIQUE (tenant_id, storage_profile_id, storage_key);

ALTER TABLE gov_identity_refs
    ADD CONSTRAINT gov_identity_refs_pkey PRIMARY KEY (tenant_id, account_id);

ALTER TABLE gov_notifications
    ADD CONSTRAINT gov_notifications_pkey PRIMARY KEY (id);

ALTER TABLE gov_notifications
    ADD CONSTRAINT gov_notifications_tenant_id_event_id_recipient_id_departmen_key UNIQUE (tenant_id, event_id, recipient_id, department_id);

ALTER TABLE gov_notifications
    ADD CONSTRAINT gov_notifications_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE iam_reauthentication_proofs
    ADD CONSTRAINT gov_reauth_pkey PRIMARY KEY (token_hash);

ALTER TABLE gov_revisions
    ADD CONSTRAINT gov_revisions_pkey PRIMARY KEY (tenant_id, resource_key);

ALTER TABLE gov_scopes
    ADD CONSTRAINT gov_scopes_pkey PRIMARY KEY (tenant_id, scope_key);

ALTER TABLE gov_settings_schedules
    ADD CONSTRAINT gov_settings_schedules_pkey PRIMARY KEY (id);

ALTER TABLE gov_settings_schedules
    ADD CONSTRAINT gov_settings_schedules_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE gov_storage_profiles
    ADD CONSTRAINT gov_storage_profiles_pkey PRIMARY KEY (id);

ALTER TABLE gov_storage_profiles
    ADD CONSTRAINT gov_storage_profiles_tenant_id_config_hash_key UNIQUE (tenant_id, config_hash);

ALTER TABLE gov_storage_profiles
    ADD CONSTRAINT gov_storage_profiles_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE gov_values
    ADD CONSTRAINT gov_values_pkey PRIMARY KEY (tenant_id, scope_key, config_key);

ALTER TABLE gov_versions
    ADD CONSTRAINT gov_versions_pkey PRIMARY KEY (id);

ALTER TABLE gov_versions
    ADD CONSTRAINT gov_versions_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE gov_versions
    ADD CONSTRAINT gov_versions_tenant_id_resource_key_revision_key UNIQUE (tenant_id, resource_key, revision);

ALTER TABLE iam_access_reviews
    ADD CONSTRAINT iam_access_reviews_pkey PRIMARY KEY (id);

ALTER TABLE iam_account_roles
    ADD CONSTRAINT iam_account_roles_id_key UNIQUE (id);

ALTER TABLE iam_account_roles
    ADD CONSTRAINT iam_account_roles_pkey PRIMARY KEY (tenant_id, account_id, role_id);

ALTER TABLE iam_accounts
    ADD CONSTRAINT iam_accounts_machine_principal_key UNIQUE (tenant_id, machine_principal_id);

ALTER TABLE iam_accounts
    ADD CONSTRAINT iam_accounts_pkey PRIMARY KEY (id);

ALTER TABLE iam_accounts
    ADD CONSTRAINT iam_accounts_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE iam_accounts
    ADD CONSTRAINT iam_accounts_tenant_login_account_key UNIQUE (tenant_id, login_account_id);

ALTER TABLE iam_agent_delegations
    ADD CONSTRAINT iam_agent_delegations_pkey PRIMARY KEY (id);

ALTER TABLE iam_audit_export_permits
    ADD CONSTRAINT iam_audit_export_permits_pkey PRIMARY KEY (tenant_id, account_id, membership_id, binding_id, request_id);

ALTER TABLE iam_contact_challenges
    ADD CONSTRAINT iam_contact_challenges_pkey PRIMARY KEY (id);

ALTER TABLE iam_contact_challenges
    ADD CONSTRAINT iam_contact_challenges_token_hash_key UNIQUE (token_hash);

ALTER TABLE iam_context_role_grants
    ADD CONSTRAINT iam_context_role_grants_pkey PRIMARY KEY (id);

ALTER TABLE identity_department_membership_roles
    ADD CONSTRAINT identity_department_membership_roles_id_key UNIQUE (id);

ALTER TABLE identity_department_membership_roles
    ADD CONSTRAINT identity_department_membership_roles_pkey PRIMARY KEY (tenant_id, account_id, department_id, role_id);

ALTER TABLE identity_department_memberships
    ADD CONSTRAINT identity_department_memberships_pkey PRIMARY KEY (tenant_id, account_id, department_id);

ALTER TABLE identity_departments
    ADD CONSTRAINT identity_departments_pkey PRIMARY KEY (id);

ALTER TABLE identity_departments
    ADD CONSTRAINT identity_departments_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE identity_departments
    ADD CONSTRAINT identity_departments_tenant_id_id_organization_id_key UNIQUE (tenant_id, id, organization_id);

ALTER TABLE identity_departments
    ADD CONSTRAINT identity_departments_tenant_id_organization_id_code_key UNIQUE (tenant_id, organization_id, code);

ALTER TABLE iam_disabled_resources
    ADD CONSTRAINT iam_disabled_resources_pkey PRIMARY KEY (tenant_id, resource);

ALTER TABLE iam_external_identities
    ADD CONSTRAINT iam_external_identities_pkey PRIMARY KEY (id);

ALTER TABLE iam_external_identities
    ADD CONSTRAINT iam_external_identities_provider_id_issuer_subject_key UNIQUE (provider_id, issuer, subject);

ALTER TABLE iam_external_identity_proofs
    ADD CONSTRAINT iam_external_identity_proofs_pkey PRIMARY KEY (proof_hash);

ALTER TABLE iam_external_identity_reviews
    ADD CONSTRAINT iam_external_identity_reviews_pkey PRIMARY KEY (id);

ALTER TABLE iam_federation_assertion_uses
    ADD CONSTRAINT iam_federation_assertion_uses_pkey PRIMARY KEY (provider_id, assertion_hash);

ALTER TABLE iam_federation_flows
    ADD CONSTRAINT iam_federation_flows_pkey PRIMARY KEY (state_hash);

ALTER TABLE iam_identity_bindings
    ADD CONSTRAINT iam_identity_bindings_pkey PRIMARY KEY (id);

ALTER TABLE iam_identity_bindings
    ADD CONSTRAINT iam_identity_bindings_tenant_id_principal_id_id_key UNIQUE (tenant_id, principal_id, id);

ALTER TABLE iam_identity_bindings
    ADD CONSTRAINT iam_identity_bindings_tenant_id_principal_id_identity_type__key UNIQUE (tenant_id, principal_id, identity_type, identity_version, entity_id);

ALTER TABLE iam_login_accounts
    ADD CONSTRAINT iam_login_accounts_pkey PRIMARY KEY (id);

ALTER TABLE iam_login_accounts
    ADD CONSTRAINT iam_login_accounts_realm_username_key UNIQUE (realm, username);

ALTER TABLE iam_login_associations
    ADD CONSTRAINT iam_login_associations_pkey PRIMARY KEY (id);

ALTER TABLE iam_login_attempts
    ADD CONSTRAINT iam_login_attempts_pkey PRIMARY KEY (attempt_key);

ALTER TABLE iam_login_merge_reviews
    ADD CONSTRAINT iam_login_merge_reviews_pkey PRIMARY KEY (id);

ALTER TABLE iam_login_sessions
    ADD CONSTRAINT iam_login_sessions_pkey PRIMARY KEY (token_hash);

ALTER TABLE iam_machine_credentials
    ADD CONSTRAINT iam_machine_credentials_pkey PRIMARY KEY (id);

ALTER TABLE iam_machine_credentials
    ADD CONSTRAINT iam_machine_credentials_token_hash_key UNIQUE (token_hash);

ALTER TABLE iam_membership_invitations
    ADD CONSTRAINT iam_membership_invitations_pkey PRIMARY KEY (id);

ALTER TABLE iam_membership_invitations
    ADD CONSTRAINT iam_membership_invitations_token_hash_key UNIQUE (token_hash);

ALTER TABLE identity_organizations
    ADD CONSTRAINT identity_organizations_pkey PRIMARY KEY (id);

ALTER TABLE identity_organizations
    ADD CONSTRAINT identity_organizations_tenant_id_key UNIQUE (tenant_id);

ALTER TABLE identity_organizations
    ADD CONSTRAINT identity_organizations_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE iam_password_credentials
    ADD CONSTRAINT iam_password_credentials_pkey PRIMARY KEY (login_account_id);

ALTER TABLE iam_pending_registrations
    ADD CONSTRAINT iam_pending_registrations_pkey PRIMARY KEY (login_account_id);

ALTER TABLE iam_policy_heads
    ADD CONSTRAINT iam_policy_heads_pkey PRIMARY KEY (tenant_id);

ALTER TABLE iam_policy_versions
    ADD CONSTRAINT iam_policy_versions_pkey PRIMARY KEY (id);

ALTER TABLE iam_policy_versions
    ADD CONSTRAINT iam_policy_versions_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE iam_principals
    ADD CONSTRAINT iam_principals_pkey PRIMARY KEY (id);

ALTER TABLE iam_recovery_codes
    ADD CONSTRAINT iam_recovery_codes_pkey PRIMARY KEY (login_account_id, code_hash);

ALTER TABLE iam_roles
    ADD CONSTRAINT iam_roles_pkey PRIMARY KEY (id);

ALTER TABLE iam_roles
    ADD CONSTRAINT iam_roles_tenant_id_code_key UNIQUE (tenant_id, code);

ALTER TABLE iam_roles
    ADD CONSTRAINT iam_roles_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE iam_security_checkpoints
    ADD CONSTRAINT iam_security_checkpoints_pkey PRIMARY KEY (last_id);

ALTER TABLE iam_security_event_archive
    ADD CONSTRAINT iam_security_event_archive_pkey PRIMARY KEY (id);

ALTER TABLE iam_security_events
    ADD CONSTRAINT iam_security_events_pkey PRIMARY KEY (id);

ALTER TABLE iam_security_retention_receipts
    ADD CONSTRAINT iam_security_retention_receipts_pkey PRIMARY KEY (checkpoint_id);

ALTER TABLE iam_sessions
    ADD CONSTRAINT iam_sessions_pkey PRIMARY KEY (token_hash);

ALTER TABLE iam_sessions
    ADD CONSTRAINT iam_sessions_public_id_key UNIQUE (public_id);

ALTER TABLE iam_temporary_requests
    ADD CONSTRAINT iam_temporary_requests_pkey PRIMARY KEY (id);

ALTER TABLE iam_temporary_requests
    ADD CONSTRAINT iam_temporary_tenant_id_unique UNIQUE (tenant_id, id);

ALTER TABLE iam_tenant_memberships
    ADD CONSTRAINT iam_tenant_memberships_pkey PRIMARY KEY (id);

ALTER TABLE iam_tenant_memberships
    ADD CONSTRAINT iam_tenant_memberships_tenant_id_principal_id_key UNIQUE (tenant_id, principal_id);

ALTER TABLE iam_tenants
    ADD CONSTRAINT iam_tenants_pkey PRIMARY KEY (id);

ALTER TABLE iam_totp_credentials
    ADD CONSTRAINT iam_totp_credentials_pkey PRIMARY KEY (login_account_id);

ALTER TABLE iam_verified_contacts
    ADD CONSTRAINT iam_verified_contacts_pkey PRIMARY KEY (login_account_id, channel);

ALTER TABLE identity_account_profiles
    ADD CONSTRAINT identity_account_profiles_pkey PRIMARY KEY (id);

ALTER TABLE identity_account_profiles
    ADD CONSTRAINT identity_account_profiles_tenant_id_account_id_key UNIQUE (tenant_id, account_id);

ALTER TABLE kb_chunks
    ADD CONSTRAINT kb_chunks_document_id_ordinal_key UNIQUE (document_id, ordinal);

ALTER TABLE kb_chunks
    ADD CONSTRAINT kb_chunks_pkey PRIMARY KEY (id);

ALTER TABLE kb_documents
    ADD CONSTRAINT kb_documents_pkey PRIMARY KEY (id);

ALTER TABLE kb_documents
    ADD CONSTRAINT kb_documents_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE mail_channels
    ADD CONSTRAINT mail_channels_pkey PRIMARY KEY (id);

ALTER TABLE mail_channels
    ADD CONSTRAINT mail_channels_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE mail_channels
    ADD CONSTRAINT mail_channels_tenant_id_mailbox_id_direction_key UNIQUE (tenant_id, mailbox_id, direction);

ALTER TABLE mail_credentials
    ADD CONSTRAINT mail_credentials_pkey PRIMARY KEY (id);

ALTER TABLE mail_credentials
    ADD CONSTRAINT mail_credentials_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE mail_credentials
    ADD CONSTRAINT mail_credentials_tenant_id_mailbox_id_purpose_key UNIQUE (tenant_id, mailbox_id, purpose);

ALTER TABLE mail_inbox_syncs
    ADD CONSTRAINT mail_inbox_syncs_pkey PRIMARY KEY (id);

ALTER TABLE mail_inbox_syncs
    ADD CONSTRAINT mail_inbox_syncs_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE mail_mailboxes
    ADD CONSTRAINT mail_mailboxes_pkey PRIMARY KEY (id);

ALTER TABLE mail_mailboxes
    ADD CONSTRAINT mail_mailboxes_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE mail_message_attachments
    ADD CONSTRAINT mail_message_attachments_pkey PRIMARY KEY (id);

ALTER TABLE mail_message_attachments
    ADD CONSTRAINT mail_message_attachments_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE mail_message_attachments
    ADD CONSTRAINT mail_message_attachments_tenant_id_message_id_file_id_key UNIQUE (tenant_id, message_id, file_id);

ALTER TABLE mail_message_recipients
    ADD CONSTRAINT mail_message_recipients_pkey PRIMARY KEY (id);

ALTER TABLE mail_message_recipients
    ADD CONSTRAINT mail_message_recipients_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE mail_message_recipients
    ADD CONSTRAINT mail_message_recipients_tenant_id_message_id_kind_address_key UNIQUE (tenant_id, message_id, kind, address);

ALTER TABLE mail_messages
    ADD CONSTRAINT mail_messages_pkey PRIMARY KEY (id);

ALTER TABLE mail_messages
    ADD CONSTRAINT mail_messages_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE mail_messages
    ADD CONSTRAINT mail_messages_tenant_id_mailbox_id_direction_provider_uid_key UNIQUE (tenant_id, mailbox_id, direction, provider_uid);

ALTER TABLE mail_outbound_deliveries
    ADD CONSTRAINT mail_outbound_deliveries_pkey PRIMARY KEY (id);

ALTER TABLE mail_outbound_deliveries
    ADD CONSTRAINT mail_outbound_deliveries_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE mail_outbound_deliveries
    ADD CONSTRAINT mail_outbound_deliveries_tenant_id_message_id_key UNIQUE (tenant_id, message_id);

ALTER TABLE mcp_call_audits
    ADD CONSTRAINT mcp_call_audits_pkey PRIMARY KEY (id);

ALTER TABLE mcp_token_tools
    ADD CONSTRAINT mcp_token_tools_pkey PRIMARY KEY (token_id, tool);

ALTER TABLE mcp_tokens
    ADD CONSTRAINT mcp_tokens_pkey PRIMARY KEY (id);

ALTER TABLE mcp_tokens
    ADD CONSTRAINT mcp_tokens_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE mcp_tokens
    ADD CONSTRAINT mcp_tokens_token_hash_key UNIQUE (token_hash);

ALTER TABLE memory_access_audit
    ADD CONSTRAINT memory_access_audit_pkey PRIMARY KEY (id);

ALTER TABLE memory_corrections
    ADD CONSTRAINT memory_corrections_pkey PRIMARY KEY (id);

ALTER TABLE memory_corrections
    ADD CONSTRAINT memory_corrections_tenant_id_memory_id_previous_version_key UNIQUE (tenant_id, memory_id, previous_version);

ALTER TABLE memory_event_inbox
    ADD CONSTRAINT memory_event_inbox_pkey PRIMARY KEY (source_event_id);

ALTER TABLE memory_event_inbox
    ADD CONSTRAINT memory_event_inbox_tenant_id_event_id_key UNIQUE (tenant_id, event_id);

ALTER TABLE memory_events
    ADD CONSTRAINT memory_events_pkey PRIMARY KEY (id);

ALTER TABLE memory_events
    ADD CONSTRAINT memory_events_tenant_id_event_id_key UNIQUE (tenant_id, event_id);

ALTER TABLE memory_items
    ADD CONSTRAINT memory_items_pkey PRIMARY KEY (id);

ALTER TABLE memory_items
    ADD CONSTRAINT memory_items_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE memory_partition_policies
    ADD CONSTRAINT memory_partition_policies_pkey PRIMARY KEY (id);

ALTER TABLE memory_partition_policies
    ADD CONSTRAINT memory_partition_policies_tenant_id_partition_key UNIQUE (tenant_id, partition);

ALTER TABLE memory_relations
    ADD CONSTRAINT memory_relations_pkey PRIMARY KEY (id);

ALTER TABLE memory_relations
    ADD CONSTRAINT memory_relations_tenant_id_source_memory_id_target_memory_i_key UNIQUE (tenant_id, source_memory_id, target_memory_id, relation_type);

ALTER TABLE memory_sources
    ADD CONSTRAINT memory_sources_pkey PRIMARY KEY (id);

ALTER TABLE memory_sources
    ADD CONSTRAINT memory_sources_tenant_id_memory_id_event_id_key UNIQUE (tenant_id, memory_id, event_id);

ALTER TABLE orch_checkpoints
    ADD CONSTRAINT orch_checkpoints_pkey PRIMARY KEY (tenant_id, run_id, checkpoint_key);

ALTER TABLE project_members
    ADD CONSTRAINT project_members_pkey PRIMARY KEY (id);

ALTER TABLE project_members
    ADD CONSTRAINT project_members_tenant_id_project_id_account_id_key UNIQUE (tenant_id, project_id, account_id);

ALTER TABLE project_plans
    ADD CONSTRAINT project_plans_pkey PRIMARY KEY (id);

ALTER TABLE project_plans
    ADD CONSTRAINT project_plans_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE project_task_briefs
    ADD CONSTRAINT project_task_briefs_pkey PRIMARY KEY (id);

ALTER TABLE project_task_briefs
    ADD CONSTRAINT project_task_briefs_tenant_id_task_id_key UNIQUE (tenant_id, task_id);

ALTER TABLE project_tasks
    ADD CONSTRAINT project_tasks_pkey PRIMARY KEY (id);

ALTER TABLE project_tasks
    ADD CONSTRAINT project_tasks_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE projects
    ADD CONSTRAINT projects_pkey PRIMARY KEY (id);

ALTER TABLE projects
    ADD CONSTRAINT projects_tenant_id_code_key UNIQUE (tenant_id, code);

ALTER TABLE projects
    ADD CONSTRAINT projects_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE runtime_application_changes
    ADD CONSTRAINT runtime_application_changes_pkey PRIMARY KEY (id);

ALTER TABLE runtime_application_heads
    ADD CONSTRAINT runtime_application_heads_pkey PRIMARY KEY (tenant_id, application_id);

ALTER TABLE runtime_application_states
    ADD CONSTRAINT runtime_application_states_pkey PRIMARY KEY (tenant_id, application_id);

ALTER TABLE runtime_application_versions
    ADD CONSTRAINT runtime_application_versions_pkey PRIMARY KEY (tenant_id, application_id, version);

ALTER TABLE runtime_deployment_snapshots
    ADD CONSTRAINT runtime_deployment_snapshots_pkey PRIMARY KEY (digest);

ALTER TABLE runtime_installation_history
    ADD CONSTRAINT runtime_installation_history_pkey PRIMARY KEY (id);

ALTER TABLE runtime_installation_templates
    ADD CONSTRAINT runtime_installation_templates_application_id_key UNIQUE (application_id);

ALTER TABLE runtime_installation_templates
    ADD CONSTRAINT runtime_installation_templates_pkey PRIMARY KEY (plugin_id);

ALTER TABLE runtime_installations
    ADD CONSTRAINT runtime_installations_pkey PRIMARY KEY (tenant_id, plugin_id);

ALTER TABLE runtime_invocation_dependencies
    ADD CONSTRAINT runtime_invocation_dependencies_pkey PRIMARY KEY (tenant_id, invocation_id, plugin_id);

ALTER TABLE runtime_run_dependencies
    ADD CONSTRAINT runtime_run_dependencies_pkey PRIMARY KEY (tenant_id, run_id, plugin_id);

ALTER TABLE technical_demo_records
    ADD CONSTRAINT technical_demo_records_pkey PRIMARY KEY (id);

CREATE INDEX ai_conversations_owner ON ai_conversations USING btree (tenant_id, account_id, updated_at DESC);

CREATE INDEX ai_runs_owner ON ai_runs USING btree (tenant_id, account_id, id DESC);

CREATE INDEX ai_trigger_executions_recent ON ai_trigger_executions USING btree (tenant_id, id DESC);

CREATE INDEX articles_owner ON articles USING btree (tenant_id, created_by, id DESC);

CREATE INDEX automation_runs_active ON automation_runs USING btree (tenant_id, task_id) WHERE (status = ANY (ARRAY['queued'::text, 'running'::text, 'awaiting_approval'::text]));

CREATE INDEX automation_runs_queue ON automation_runs USING btree (id) WHERE (status = 'queued'::text);

CREATE INDEX automation_runs_task ON automation_runs USING btree (tenant_id, task_id, id DESC);

CREATE INDEX automation_tasks_due ON automation_tasks USING btree (next_run_at, id) WHERE (enabled AND (next_run_at IS NOT NULL));

CREATE INDEX automation_tasks_owner ON automation_tasks USING btree (tenant_id, owner_id, id DESC);

CREATE INDEX cap_approvals_owner ON cap_approvals USING btree (tenant_id, account_id, status, id DESC);

CREATE UNIQUE INDEX cap_approvals_pending ON cap_approvals USING btree (tenant_id, capability, account_id, arguments_digest, action_key, identity_binding_id) NULLS NOT DISTINCT WHERE (status = 'pending'::text);

CREATE INDEX cap_events_recent ON cap_events USING btree (tenant_id, id DESC);

CREATE INDEX cap_invocations_correlation ON cap_invocations USING btree (tenant_id, correlation_id);

CREATE UNIQUE INDEX cap_invocations_idempotency ON cap_invocations USING btree (tenant_id, capability, idempotency_key) WHERE (idempotency_key <> ''::text);

CREATE INDEX cap_invocations_owner ON cap_invocations USING btree (tenant_id, account_id, id DESC);

CREATE INDEX cap_outbox_identity_revocations ON cap_outbox USING btree (tenant_id, event_type, aggregate_id);

CREATE INDEX cap_outbox_pending ON cap_outbox USING btree (status, available_at, id);

CREATE INDEX drive_file_folder ON drive_files USING btree (tenant_id, space_id, folder_id, id) WHERE (deleted_at IS NULL);

CREATE UNIQUE INDEX drive_file_name ON drive_files USING btree (tenant_id, space_id, COALESCE(folder_id, (0)::bigint), name) WHERE (deleted_at IS NULL);

CREATE INDEX drive_file_trash_batch ON drive_files USING btree (tenant_id, trash_batch_id) WHERE (trash_batch_id IS NOT NULL);

CREATE UNIQUE INDEX drive_file_trash_root ON drive_files USING btree (tenant_id, trash_batch_id) WHERE trash_root;

CREATE UNIQUE INDEX drive_folder_name ON drive_folders USING btree (tenant_id, space_id, COALESCE(parent_id, (0)::bigint), name) WHERE (deleted_at IS NULL);

CREATE INDEX drive_folder_parent ON drive_folders USING btree (tenant_id, space_id, parent_id, id) WHERE (deleted_at IS NULL);

CREATE INDEX drive_folder_trash_batch ON drive_folders USING btree (tenant_id, trash_batch_id) WHERE (trash_batch_id IS NOT NULL);

CREATE UNIQUE INDEX drive_folder_trash_root ON drive_folders USING btree (tenant_id, trash_batch_id) WHERE trash_root;

CREATE UNIQUE INDEX drive_member_account ON drive_members USING btree (tenant_id, space_id, account_id) WHERE (account_id IS NOT NULL);

CREATE INDEX drive_member_by_account ON drive_members USING btree (tenant_id, account_id, space_id) WHERE (account_id IS NOT NULL);

CREATE INDEX drive_member_by_department ON drive_members USING btree (tenant_id, department_id, space_id) WHERE (department_id IS NOT NULL);

CREATE UNIQUE INDEX drive_member_department ON drive_members USING btree (tenant_id, space_id, department_id) WHERE (department_id IS NOT NULL);

CREATE UNIQUE INDEX drive_personal_owner ON drive_spaces USING btree (tenant_id, owner_id) WHERE (kind = 'personal'::text);

CREATE INDEX drive_trash_expiry ON drive_trash_batches USING btree (expires_at, tenant_id, id);

CREATE INDEX drive_upload_expiry ON drive_uploads USING btree (state, expires_at, tenant_id, id);

CREATE INDEX example_orders_supplier ON example_orders USING btree (tenant_id, supplier_id, id);

CREATE INDEX example_orders_tenant ON example_orders USING btree (tenant_id, id);

CREATE INDEX example_records_owner ON example_records USING btree (tenant_id, id DESC);

CREATE UNIQUE INDEX gov_announcement_recipient ON gov_notifications USING btree (tenant_id, announcement_id, recipient_id);

CREATE INDEX gov_deliveries_pending ON gov_deliveries USING btree (status, next_attempt_at, lease_until);

CREATE INDEX gov_events_pending ON gov_events USING btree (processed, lease_until);

CREATE INDEX gov_inbox ON gov_notifications USING btree (tenant_id, recipient_id, department_id, created_at DESC);

CREATE UNIQUE INDEX gov_one_pending_settings_schedule ON gov_settings_schedules USING btree (tenant_id, draft_id) WHERE (status = 'pending'::text);

CREATE INDEX gov_settings_schedules_due ON gov_settings_schedules USING btree (scheduled_at, id) WHERE (status = 'pending'::text);

CREATE UNIQUE INDEX gov_temporary_event_recipient ON gov_notifications USING btree (tenant_id, event_id, recipient_id) WHERE ((event_id IS NOT NULL) AND (department_id IS NULL));

CREATE INDEX iam_access_reviews_list ON iam_access_reviews USING btree (tenant_id, status, id);

CREATE UNIQUE INDEX iam_access_reviews_open ON iam_access_reviews USING btree (tenant_id, kind, target_id) WHERE (status = 'pending'::text);

CREATE INDEX iam_accounts_login_placement ON iam_accounts USING btree (login_account_id);

CREATE INDEX iam_contact_challenge_delivery ON iam_contact_challenges USING btree (delivery_due_at, id) WHERE ((delivered_at IS NULL) AND (consumed_at IS NULL));

CREATE INDEX iam_contact_challenges_latest ON iam_contact_challenges USING btree (login_account_id, purpose, channel, created_at DESC);

CREATE INDEX iam_context_role_grants_current ON iam_context_role_grants USING btree (tenant_id, principal_id, expires_at) WHERE (revoked_at IS NULL);

CREATE INDEX iam_external_identities_login ON iam_external_identities USING btree (login_account_id);

CREATE INDEX iam_external_review_tenant ON iam_external_identity_reviews USING btree (tenant_id, id);

CREATE INDEX iam_federation_assertion_uses_expiry ON iam_federation_assertion_uses USING btree (expires_at);

CREATE INDEX iam_identity_bindings_provider ON iam_identity_bindings USING btree (tenant_id, identity_type, identity_version, entity_id) WHERE (status = 'active'::text);

CREATE INDEX iam_login_accounts_principal ON iam_login_accounts USING btree (principal_id);

CREATE UNIQUE INDEX iam_login_association_active ON iam_login_associations USING btree (associated_login_id) WHERE (revoked_at IS NULL);

CREATE INDEX iam_login_association_owner ON iam_login_associations USING btree (principal_id, primary_login_id, id);

CREATE INDEX iam_login_merge_review_queue ON iam_login_merge_reviews USING btree (tenant_id, created_at DESC, id);

CREATE INDEX iam_login_sessions_by_login ON iam_login_sessions USING btree (login_account_id);

CREATE INDEX iam_machine_credentials_account ON iam_machine_credentials USING btree (tenant_id, account_id) WHERE (revoked_at IS NULL);

CREATE INDEX iam_memberships_scope ON identity_department_memberships USING btree (tenant_id, organization_id, department_id);

CREATE INDEX iam_policy_version_tenant_history ON iam_policy_versions USING btree (tenant_id, id DESC);

CREATE UNIQUE INDEX iam_roles_tenant_template ON iam_roles USING btree (tenant_id, template_key) WHERE (template_key <> 'custom'::text);

CREATE INDEX iam_security_event_archive_tenant_id_account_id_id_idx ON iam_security_event_archive USING btree (tenant_id, account_id, id DESC);

CREATE INDEX iam_security_event_archive_tenant_id_id_idx ON iam_security_event_archive USING btree (tenant_id, id DESC);

CREATE INDEX iam_security_events_account ON iam_security_events USING btree (tenant_id, account_id, id DESC);

CREATE INDEX iam_security_events_tenant ON iam_security_events USING btree (tenant_id, id DESC);

CREATE INDEX iam_sessions_account ON iam_sessions USING btree (tenant_id, account_id);

CREATE INDEX iam_temporary_active ON iam_temporary_requests USING btree (tenant_id, applicant_id, expires_at) WHERE (status = 'approved'::text);

CREATE INDEX iam_temporary_applicant ON iam_temporary_requests USING btree (tenant_id, applicant_id, created_at DESC);

CREATE INDEX iam_tenant_memberships_principal ON iam_tenant_memberships USING btree (principal_id);

CREATE INDEX kb_chunks_content_trgm ON kb_chunks USING gin (content gin_trgm_ops);

CREATE INDEX kb_chunks_tenant_document ON kb_chunks USING btree (tenant_id, document_id);

CREATE INDEX mail_channels_due ON mail_channels USING btree (tenant_id, next_sync_at, id) WHERE (direction = 'receive'::text);

CREATE INDEX mail_inbox_syncs_mailbox ON mail_inbox_syncs USING btree (tenant_id, mailbox_id, started_at DESC);

CREATE UNIQUE INDEX mail_mailboxes_address ON mail_mailboxes USING btree (tenant_id, owner_account_id, lower(email_address));

CREATE UNIQUE INDEX mail_mailboxes_default_send ON mail_mailboxes USING btree (tenant_id, owner_account_id) WHERE default_send;

CREATE INDEX mail_mailboxes_owner ON mail_mailboxes USING btree (tenant_id, owner_account_id, id DESC);

CREATE INDEX mail_message_attachments_message ON mail_message_attachments USING btree (tenant_id, message_id);

CREATE INDEX mail_message_recipients_message ON mail_message_recipients USING btree (tenant_id, message_id, kind);

CREATE INDEX mail_messages_mailbox ON mail_messages USING btree (tenant_id, mailbox_id, direction, occurred_at DESC);

CREATE INDEX mail_messages_owner ON mail_messages USING btree (tenant_id, owner_account_id, folder, occurred_at DESC, id DESC);

CREATE INDEX mail_outbound_due ON mail_outbound_deliveries USING btree (tenant_id, next_attempt_at, id) WHERE (status = 'queued'::text);

CREATE INDEX mail_outbound_owner ON mail_outbound_deliveries USING btree (tenant_id, submitted_by, created_at DESC);

CREATE INDEX mcp_call_audits_owner ON mcp_call_audits USING btree (tenant_id, account_id, id DESC);

CREATE INDEX mcp_tokens_owner ON mcp_tokens USING btree (tenant_id, account_id, id);

CREATE INDEX memory_access_audit_actor ON memory_access_audit USING btree (tenant_id, actor_id, created_at DESC, id DESC);

CREATE INDEX memory_event_inbox_pending ON memory_event_inbox USING btree (available_at, source_event_id) WHERE ((completed_at IS NULL) AND (dead_at IS NULL));

CREATE INDEX memory_events_correlation ON memory_events USING btree (tenant_id, correlation_id) WHERE (correlation_id <> ''::text);

CREATE INDEX memory_events_owner_recent ON memory_events USING btree (tenant_id, actor_type, actor_id, occurred_at DESC, id DESC);

CREATE INDEX memory_events_resource_recent ON memory_events USING btree (tenant_id, resource_type, resource_id, occurred_at DESC, id DESC);

CREATE INDEX memory_items_owner ON memory_items USING btree (tenant_id, owner_id, partition, id DESC) WHERE (status = 'active'::text);

CREATE INDEX memory_items_scope ON memory_items USING btree (tenant_id, scope_type, scope_id, partition, id DESC) WHERE (status = 'active'::text);

CREATE INDEX memory_relations_target ON memory_relations USING btree (tenant_id, target_memory_id);

CREATE INDEX orch_checkpoint_queue ON orch_checkpoints USING btree (lease_until) WHERE (checkpoint_key = 'manifest'::text);

CREATE INDEX project_members_account ON project_members USING btree (tenant_id, account_id, project_id);

CREATE INDEX project_plans_project ON project_plans USING btree (tenant_id, project_id, id DESC);

CREATE INDEX project_tasks_assignee ON project_tasks USING btree (tenant_id, assignee_id, status);

CREATE INDEX project_tasks_project ON project_tasks USING btree (tenant_id, project_id, status, id DESC);

CREATE INDEX projects_owner ON projects USING btree (tenant_id, owner_id, id DESC);

CREATE INDEX runtime_application_changes_history ON runtime_application_changes USING btree (tenant_id, application_id, id DESC);

CREATE UNIQUE INDEX runtime_application_route_root ON runtime_application_heads USING btree (tenant_id, route_root) WHERE (route_root <> ''::text);

CREATE UNIQUE INDEX runtime_deployment_identity ON runtime_deployment_snapshots USING btree (plugin_id, plugin_version, application_id);

CREATE INDEX technical_demo_scope ON technical_demo_records USING btree (tenant_id, owner_id);

CREATE FUNCTION cap_execution_plan_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF TG_OP='DELETE' OR OLD.status IN ('published','archived') OR
 (NEW.tenant_id,NEW.plan_id,NEW.version,NEW.capability,NEW.capability_version,NEW.name,NEW.mode,NEW.implementation_ref,NEW.contract_hash,NEW.created_by,NEW.created_at)
 IS DISTINCT FROM
 (OLD.tenant_id,OLD.plan_id,OLD.version,OLD.capability,OLD.capability_version,OLD.name,OLD.mode,OLD.implementation_ref,OLD.contract_hash,OLD.created_by,OLD.created_at)
 THEN RAISE EXCEPTION 'immutable execution plan' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION gov_identity_remember() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 INSERT INTO gov_identity_refs(tenant_id,account_id) VALUES(NEW.tenant_id,NEW.id) ON CONFLICT DO NOTHING;
 RETURN NEW;
END $$;

CREATE FUNCTION gov_temporary_event() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  INSERT INTO gov_events(tenant_id,request_id,event) VALUES(NEW.tenant_id,NEW.id,'requested') ON CONFLICT DO NOTHING;
 ELSIF NEW.status<>OLD.status AND NEW.status IN('approved','rejected','revoked') THEN
  INSERT INTO gov_events(tenant_id,request_id,event) VALUES(NEW.tenant_id,NEW.id,NEW.status) ON CONFLICT DO NOTHING;
 END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION iam_audit_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    RAISE EXCEPTION 'security audit records are append-only' USING ERRCODE = '42501';
END $$;

CREATE FUNCTION iam_audit_invocation() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'INSERT' OR OLD.status IS DISTINCT FROM NEW.status THEN
        INSERT INTO iam_security_events(tenant_id, account_id, binding_id, action, outcome, reason,
            correlation_id, resource_type, resource_id)
        VALUES (NEW.tenant_id, NEW.account_id, NEW.identity_binding_id, NEW.capability,
            NEW.status, NEW.error_code, NEW.trace_id, 'invocation', NEW.invocation_id);
    END IF;
    RETURN NULL;
END $$;

CREATE FUNCTION iam_audit_resource_event() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    INSERT INTO iam_security_events(tenant_id, account_id, binding_id, action, outcome, correlation_id, resource_type, resource_id)
    VALUES (NEW.tenant_id, NEW.account_id, NEW.identity_binding_id, NEW.event_type, 'committed',
        NEW.correlation_id, NEW.aggregate_type, NEW.aggregate_id);
    RETURN NULL;
END $$;

CREATE FUNCTION iam_audit_session() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        INSERT INTO iam_security_events(tenant_id, account_id, binding_id, action, outcome, resource_type, resource_id)
        VALUES (OLD.tenant_id, OLD.account_id, OLD.identity_binding_id,
            'identity.session.end', 'revoked', 'session', OLD.public_id::text);
    ELSIF TG_OP = 'INSERT' OR OLD.csrf_token IS DISTINCT FROM NEW.csrf_token THEN
        INSERT INTO iam_security_events(tenant_id, account_id, binding_id, action, outcome, correlation_id, resource_type, resource_id)
        VALUES (NEW.tenant_id, NEW.account_id, NEW.identity_binding_id,
            CASE WHEN TG_OP = 'INSERT' THEN 'identity.session.start' ELSE 'identity.session.switch' END,
            'allowed', coalesce(current_setting('horizon.request_id', true), ''), 'session', NEW.public_id::text);
    END IF;
    RETURN NULL;
END $$;

CREATE FUNCTION iam_capture_audit_authority() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    principal uuid;
    login uuid;
    call cap_invocations%ROWTYPE;
BEGIN
    SELECT coalesce(account.machine_principal_id, subject.principal_id), subject.id
    INTO principal, login
    FROM iam_accounts account LEFT JOIN iam_login_accounts subject ON subject.id = account.login_account_id
    WHERE account.tenant_id = NEW.tenant_id AND account.id = NEW.account_id;

    IF NEW.resource_type = 'invocation' THEN
        SELECT * INTO call FROM cap_invocations
        WHERE tenant_id = NEW.tenant_id AND invocation_id = NEW.resource_id AND account_id = NEW.account_id;
    END IF;

    NEW.authority := nullif(jsonb_strip_nulls(jsonb_build_object(
        'principalId', principal, 'loginAccountId', login,
        'origin', call.origin, 'capabilityVersion', call.capability_version,
        'machineCredentialId', call.machine_credential_id,
        'delegationId', call.agent_delegation_id
    )) || coalesce(NEW.authority, '{}'::jsonb), '{}'::jsonb);
    RETURN NEW;
END $$;

CREATE FUNCTION iam_clear_contact_challenges() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_TABLE_NAME = 'iam_principals' THEN
        UPDATE iam_contact_challenges challenge SET consumed_at = statement_timestamp(), sealed_token = ''
        FROM iam_login_accounts login WHERE login.principal_id = OLD.id
          AND challenge.login_account_id = login.id AND challenge.consumed_at IS NULL;
    ELSIF TG_TABLE_NAME = 'iam_login_accounts' THEN
        UPDATE iam_contact_challenges SET consumed_at = statement_timestamp(), sealed_token = ''
        WHERE login_account_id = OLD.id AND consumed_at IS NULL;
    ELSE
        UPDATE iam_contact_challenges SET consumed_at = statement_timestamp(), sealed_token = ''
        WHERE login_account_id = OLD.login_account_id AND consumed_at IS NULL;
    END IF;
    RETURN NULL;
END $$;

CREATE FUNCTION iam_mark_global_principal_revocation() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    UPDATE iam_tenant_memberships SET access_revoked_at = statement_timestamp()
    WHERE principal_id = NEW.id;
    RETURN NULL;
END $$;

CREATE FUNCTION iam_mark_membership_revocation() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.access_revoked_at := statement_timestamp();
    RETURN NEW;
END $$;

CREATE FUNCTION iam_mark_tenant_revocation() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    UPDATE iam_tenant_memberships SET access_revoked_at = statement_timestamp() WHERE tenant_id = NEW.id;
    RETURN NULL;
END $$;

CREATE FUNCTION iam_require_audit_archive() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM iam_security_event_archive archived
        WHERE archived.id = OLD.id AND to_jsonb(archived) = to_jsonb(OLD)
    ) THEN
        RAISE EXCEPTION 'security event must be archived before removal' USING ERRCODE = '42501';
    END IF;
    RETURN OLD;
END $$;

CREATE FUNCTION iam_require_audit_retention_receipt() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM iam_security_checkpoints checkpoint
        JOIN iam_security_retention_receipts receipt ON receipt.checkpoint_id = checkpoint.last_id
        WHERE OLD.id > checkpoint.previous_id AND OLD.id <= checkpoint.last_id
            AND OLD.created_at < receipt.cutoff AND receipt.cutoff <= now()
    ) THEN
        RAISE EXCEPTION 'security archive record requires a retention receipt' USING ERRCODE = '42501';
    END IF;
    RETURN OLD;
END $$;

CREATE FUNCTION iam_revoke_login_sessions_after_password_delete() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    DELETE FROM iam_sessions session USING iam_accounts placement
    WHERE placement.login_account_id = OLD.login_account_id AND
          session.tenant_id = placement.tenant_id AND session.account_id = placement.id;
    UPDATE mcp_tokens token SET revoked_at = statement_timestamp() FROM iam_accounts placement
    WHERE placement.login_account_id = OLD.login_account_id AND
          token.tenant_id = placement.tenant_id AND token.account_id = placement.id AND token.revoked_at IS NULL;
    RETURN NULL;
END $$;

CREATE FUNCTION iam_revoke_membership_runtime_access() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    accounts bigint[];
BEGIN
    SELECT array_agg(placement.id) INTO accounts
    FROM iam_accounts placement
    LEFT JOIN iam_login_accounts login ON login.id = placement.login_account_id
    WHERE placement.tenant_id = OLD.tenant_id
      AND COALESCE(login.principal_id, placement.machine_principal_id) = OLD.principal_id;

    UPDATE iam_machine_credentials SET revoked_at = statement_timestamp()
    WHERE tenant_id = OLD.tenant_id AND (account_id = ANY(accounts) OR issuer_id = ANY(accounts)) AND revoked_at IS NULL;
    UPDATE iam_agent_delegations SET revoked_at = statement_timestamp()
    WHERE tenant_id = OLD.tenant_id AND (agent_account_id = ANY(accounts) OR delegator_account_id = ANY(accounts)) AND revoked_at IS NULL;
    UPDATE cap_approvals SET status = 'expired', execution_code = 'membership_inactive'
    WHERE tenant_id = OLD.tenant_id AND account_id = ANY(accounts) AND status IN ('pending', 'approved');
    UPDATE gov_settings_schedules SET status = 'cancelled', finished_at = statement_timestamp()
    WHERE tenant_id = OLD.tenant_id AND actor_id = ANY(accounts) AND status = 'pending';
    UPDATE automation_runs run SET status = 'cancelled', error_code = 'membership_inactive', finished_at = statement_timestamp()
    FROM automation_tasks task
    WHERE run.tenant_id = OLD.tenant_id AND run.tenant_id = task.tenant_id AND run.task_id = task.id
      AND task.owner_id = ANY(accounts) AND run.status IN ('queued', 'running', 'awaiting_approval');
    UPDATE automation_tasks SET enabled = false, next_run_at = NULL, updated_at = statement_timestamp()
    WHERE tenant_id = OLD.tenant_id AND owner_id = ANY(accounts) AND enabled;
    UPDATE ai_runs SET status = 'cancelled', error_code = 'membership_inactive', finished_at = statement_timestamp()
    WHERE tenant_id = OLD.tenant_id AND account_id = ANY(accounts) AND status IN ('running', 'awaiting_approval');
    RETURN NULL;
END $$;

CREATE FUNCTION iam_revoke_password_sessions() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 DELETE FROM iam_sessions s USING iam_accounts a
 WHERE a.login_account_id=NEW.login_account_id AND s.tenant_id=a.tenant_id AND s.account_id=a.id;
 UPDATE mcp_tokens t SET revoked_at=now() FROM iam_accounts a
 WHERE a.login_account_id=NEW.login_account_id AND t.tenant_id=a.tenant_id AND t.account_id=a.id AND t.revoked_at IS NULL;
 RETURN NULL;
END $$;

CREATE FUNCTION iam_revoke_subject_access() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    target_tenant text;
    target_subject uuid;
    target_login uuid;
BEGIN
    CASE TG_TABLE_NAME
    WHEN 'iam_tenants' THEN target_tenant := OLD.id;
    WHEN 'iam_principals' THEN target_subject := OLD.id;
    WHEN 'iam_login_accounts' THEN target_login := OLD.id;
    WHEN 'iam_tenant_memberships' THEN target_subject := OLD.principal_id; target_tenant := OLD.tenant_id;
    END CASE;

    WITH placements AS (
        SELECT placement.tenant_id, placement.id FROM iam_accounts placement
        JOIN iam_login_accounts login ON login.id = placement.login_account_id
        WHERE (target_tenant IS NULL OR placement.tenant_id = target_tenant)
          AND (target_subject IS NULL OR login.principal_id = target_subject)
          AND (target_login IS NULL OR login.id = target_login)
    ), sessions AS (
        DELETE FROM iam_sessions session USING placements
        WHERE session.tenant_id = placements.tenant_id AND session.account_id = placements.id
    )
    UPDATE mcp_tokens token SET revoked_at = now() FROM placements
    WHERE token.tenant_id = placements.tenant_id AND token.account_id = placements.id AND token.revoked_at IS NULL;
    RETURN NULL;
END $$;

CREATE FUNCTION iam_revoke_unscoped_sessions() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_TABLE_NAME = 'iam_principals' THEN
        DELETE FROM iam_login_sessions session USING iam_login_accounts login
        WHERE session.login_account_id = login.id AND login.principal_id = OLD.id;
    ELSIF TG_TABLE_NAME = 'iam_login_accounts' THEN
        DELETE FROM iam_login_sessions WHERE login_account_id = OLD.id;
    ELSE
        DELETE FROM iam_login_sessions WHERE login_account_id = OLD.login_account_id;
    END IF;
    RETURN NULL;
END $$;

CREATE FUNCTION mcp_account_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.status = 'inactive' THEN
        UPDATE mcp_tokens SET revoked_at = now()
        WHERE tenant_id = NEW.tenant_id AND account_id = NEW.id AND revoked_at IS NULL;
    END IF;
    RETURN NULL;
END $$;

CREATE FUNCTION mcp_tenant_membership_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    UPDATE mcp_tokens token SET revoked_at = now()
    FROM iam_accounts account JOIN iam_login_accounts login ON login.id = account.login_account_id
    WHERE token.tenant_id = OLD.tenant_id AND account.tenant_id = token.tenant_id
      AND token.account_id = account.id AND login.principal_id = OLD.principal_id AND token.revoked_at IS NULL;
    RETURN NULL;
END $$;

CREATE FUNCTION memory_group_authorized(text, text, text, boolean, bigint[], bigint[], boolean, bigint[], bigint[], bigint) RETURNS boolean
    LANGUAGE sql STABLE
    AS $_$
 SELECT CASE $2
 WHEN 'department' THEN EXISTS (
  SELECT 1 FROM identity_departments d WHERE d.tenant_id=$1 AND d.id::text=$3
   AND ($4 OR d.organization_id=ANY($5) OR d.id=ANY($6))
   AND ($7 OR d.organization_id=ANY($8) OR d.id=ANY($9)))
 WHEN 'organization' THEN EXISTS (
  SELECT 1 FROM identity_organizations o WHERE o.tenant_id=$1 AND o.id::text=$3
   AND ($4 OR o.id=ANY($5)) AND ($7 OR o.id=ANY($8)))
 WHEN 'business_entity' THEN EXISTS (
  SELECT 1 FROM example_records e WHERE e.tenant_id=$1 AND ('example_record:'||e.id::text)=$3 AND e.created_by=$10)
 ELSE false END
$_$;

CREATE FUNCTION runtime_capture_ai_event() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE kind text; seq bigint;
BEGIN
 IF TG_OP='UPDATE' AND NEW.status=OLD.status THEN RETURN NEW; END IF;
 kind:=CASE NEW.status WHEN 'running' THEN 'RUN_STARTED' WHEN 'completed' THEN 'RUN_FINISHED'
 WHEN 'failed' THEN 'RUN_ERROR' WHEN 'cancelled' THEN 'RUN_ERROR' ELSE 'STATE_SNAPSHOT' END;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id||':run-event:'||NEW.id::text,0));
 SELECT COALESCE(MAX(sequence),0)+1 INTO seq FROM ai_run_events WHERE tenant_id=NEW.tenant_id AND run_id=NEW.id;
 INSERT INTO ai_run_events(tenant_id,run_id,sequence,event_type,payload)
 VALUES(NEW.tenant_id,NEW.id,seq,kind,jsonb_build_object('status',NEW.status,'code',NEW.error_code));
 RETURN NEW;
END $$;

CREATE FUNCTION runtime_capture_tool_event() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE task record; kind text; seq bigint;
BEGIN
 IF TG_OP='UPDATE' AND NEW.status=OLD.status THEN RETURN NEW; END IF;
 IF NEW.status='executing' THEN kind:='TOOL_CALL_START';
 ELSIF NEW.status IN ('succeeded','failed','cancelled') THEN kind:='TOOL_CALL_END';
 ELSIF NEW.status='awaiting_approval' THEN kind:='STATE_SNAPSHOT'; ELSE RETURN NEW; END IF;
 FOR task IN SELECT id FROM ai_runs WHERE tenant_id=NEW.tenant_id AND account_id=NEW.account_id
 AND correlation_id=NEW.correlation_id AND status IN ('running','awaiting_approval') ORDER BY id LOOP
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id||':run-event:'||task.id::text,0));
  SELECT COALESCE(MAX(sequence),0)+1 INTO seq FROM ai_run_events WHERE tenant_id=NEW.tenant_id AND run_id=task.id;
  INSERT INTO ai_run_events(tenant_id,run_id,sequence,event_type,payload)
  VALUES(NEW.tenant_id,task.id,seq,kind,jsonb_build_object('status',NEW.status,'toolCallId',NEW.invocation_id,'toolCallName',NEW.capability));
 END LOOP;
 RETURN NEW;
END $$;

CREATE FUNCTION runtime_definition_pointer_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF NEW.published_version<>'' AND NOT EXISTS(
  SELECT 1 FROM runtime_application_versions WHERE tenant_id=NEW.tenant_id AND application_id=NEW.application_id AND version=NEW.published_version
 ) THEN RAISE EXCEPTION 'invalid runtime published pointer' USING ERRCODE='23503'; END IF;
 IF NEW.proposal_version<>'' AND NOT EXISTS(
  SELECT 1 FROM runtime_application_versions WHERE tenant_id=NEW.tenant_id AND application_id=NEW.application_id AND version=NEW.proposal_version
 ) THEN RAISE EXCEPTION 'invalid runtime proposal pointer' USING ERRCODE='23503'; END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION runtime_immutable_definition() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN RAISE EXCEPTION 'immutable runtime definition' USING ERRCODE='23514'; END $$;

CREATE FUNCTION runtime_project_scope_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF TG_OP='DELETE' THEN
 UPDATE runtime_application_states SET revision=revision+1 WHERE tenant_id=OLD.tenant_id AND application_id='projects';
 ELSE
 UPDATE runtime_application_states SET revision=revision+1 WHERE tenant_id=NEW.tenant_id AND application_id='projects';
 END IF;
 RETURN NULL;
END $$;

CREATE TRIGGER cap_execution_plan_immutable BEFORE DELETE OR UPDATE ON cap_execution_plans FOR EACH ROW EXECUTE FUNCTION cap_execution_plan_guard();

CREATE TRIGGER gov_identity_remember_trigger AFTER INSERT ON iam_accounts FOR EACH ROW EXECUTE FUNCTION gov_identity_remember();

CREATE TRIGGER gov_temporary_event_trigger AFTER INSERT OR UPDATE ON iam_temporary_requests FOR EACH ROW EXECUTE FUNCTION gov_temporary_event();

CREATE TRIGGER iam_clear_contact_challenges_on_login_change AFTER UPDATE OF status, principal_id ON iam_login_accounts FOR EACH ROW WHEN (((old.status IS DISTINCT FROM new.status) OR (old.principal_id IS DISTINCT FROM new.principal_id))) EXECUTE FUNCTION iam_clear_contact_challenges();

CREATE TRIGGER iam_clear_contact_challenges_on_mfa_change AFTER DELETE OR UPDATE OF activated_at, sealed_secret ON iam_totp_credentials FOR EACH ROW EXECUTE FUNCTION iam_clear_contact_challenges();

CREATE TRIGGER iam_clear_contact_challenges_on_password_change AFTER DELETE OR UPDATE OF password_hash ON iam_password_credentials FOR EACH ROW EXECUTE FUNCTION iam_clear_contact_challenges();

CREATE TRIGGER iam_clear_contact_challenges_on_subject_change AFTER UPDATE OF status ON iam_principals FOR EACH ROW WHEN ((old.status IS DISTINCT FROM new.status)) EXECUTE FUNCTION iam_clear_contact_challenges();

CREATE TRIGGER iam_login_status_changed AFTER UPDATE ON iam_login_accounts FOR EACH ROW WHEN (((old.status IS DISTINCT FROM new.status) OR (old.principal_id IS DISTINCT FROM new.principal_id))) EXECUTE FUNCTION iam_revoke_subject_access();

CREATE TRIGGER iam_membership_mark_revocation BEFORE UPDATE ON iam_tenant_memberships FOR EACH ROW WHEN (((new.status IS DISTINCT FROM old.status) AND (new.status = ANY (ARRAY['suspended'::text, 'revoked'::text])))) EXECUTE FUNCTION iam_mark_membership_revocation();

CREATE TRIGGER iam_membership_removed AFTER DELETE ON iam_tenant_memberships FOR EACH ROW EXECUTE FUNCTION iam_revoke_subject_access();

CREATE TRIGGER iam_membership_runtime_status_changed AFTER UPDATE ON iam_tenant_memberships FOR EACH ROW WHEN ((new.access_revoked_at IS DISTINCT FROM old.access_revoked_at)) EXECUTE FUNCTION iam_revoke_membership_runtime_access();

CREATE TRIGGER iam_membership_status_changed AFTER UPDATE ON iam_tenant_memberships FOR EACH ROW WHEN (((old.status IS DISTINCT FROM new.status) OR (old.tenant_id IS DISTINCT FROM new.tenant_id) OR (old.principal_id IS DISTINCT FROM new.principal_id) OR (old.expires_at IS DISTINCT FROM new.expires_at))) EXECUTE FUNCTION iam_revoke_subject_access();

CREATE TRIGGER iam_password_changed AFTER UPDATE OF password_hash ON iam_password_credentials FOR EACH ROW WHEN ((new.password_hash IS DISTINCT FROM old.password_hash)) EXECUTE FUNCTION iam_revoke_password_sessions();

CREATE TRIGGER iam_password_deleted AFTER DELETE ON iam_password_credentials FOR EACH ROW EXECUTE FUNCTION iam_revoke_login_sessions_after_password_delete();

CREATE TRIGGER iam_principal_runtime_status_changed AFTER UPDATE OF status ON iam_principals FOR EACH ROW WHEN (((old.status = 'active'::text) AND (new.status = ANY (ARRAY['suspended'::text, 'inactive'::text])))) EXECUTE FUNCTION iam_mark_global_principal_revocation();

CREATE TRIGGER iam_principal_status_changed AFTER UPDATE ON iam_principals FOR EACH ROW WHEN (((old.status IS DISTINCT FROM new.status) OR (old.kind IS DISTINCT FROM new.kind))) EXECUTE FUNCTION iam_revoke_subject_access();

CREATE TRIGGER iam_revoke_login_sessions_for_account AFTER UPDATE ON iam_login_accounts FOR EACH ROW WHEN (((old.status IS DISTINCT FROM new.status) OR (old.principal_id IS DISTINCT FROM new.principal_id))) EXECUTE FUNCTION iam_revoke_unscoped_sessions();

CREATE TRIGGER iam_revoke_login_sessions_for_mfa AFTER DELETE OR UPDATE OF activated_at, sealed_secret ON iam_totp_credentials FOR EACH ROW EXECUTE FUNCTION iam_revoke_unscoped_sessions();

CREATE TRIGGER iam_revoke_login_sessions_for_password AFTER DELETE OR UPDATE OF password_hash ON iam_password_credentials FOR EACH ROW EXECUTE FUNCTION iam_revoke_unscoped_sessions();

CREATE TRIGGER iam_revoke_login_sessions_for_principal AFTER UPDATE ON iam_principals FOR EACH ROW WHEN (((old.status IS DISTINCT FROM new.status) OR (old.kind IS DISTINCT FROM new.kind))) EXECUTE FUNCTION iam_revoke_unscoped_sessions();

CREATE TRIGGER iam_security_archive_append_only BEFORE UPDATE OR TRUNCATE ON iam_security_event_archive FOR EACH STATEMENT EXECUTE FUNCTION iam_audit_append_only();

CREATE TRIGGER iam_security_archive_retention_required BEFORE DELETE ON iam_security_event_archive FOR EACH ROW EXECUTE FUNCTION iam_require_audit_retention_receipt();

CREATE TRIGGER iam_security_checkpoints_append_only BEFORE DELETE OR UPDATE OR TRUNCATE ON iam_security_checkpoints FOR EACH STATEMENT EXECUTE FUNCTION iam_audit_append_only();

CREATE TRIGGER iam_security_event_authority BEFORE INSERT ON iam_security_events FOR EACH ROW EXECUTE FUNCTION iam_capture_audit_authority();

CREATE TRIGGER iam_security_events_append_only BEFORE UPDATE OR TRUNCATE ON iam_security_events FOR EACH STATEMENT EXECUTE FUNCTION iam_audit_append_only();

CREATE TRIGGER iam_security_events_archive_required BEFORE DELETE ON iam_security_events FOR EACH ROW EXECUTE FUNCTION iam_require_audit_archive();

CREATE TRIGGER iam_security_invocation AFTER INSERT OR UPDATE OF status ON cap_invocations FOR EACH ROW EXECUTE FUNCTION iam_audit_invocation();

CREATE TRIGGER iam_security_receipt_append_only BEFORE DELETE OR UPDATE OR TRUNCATE ON iam_security_retention_receipts FOR EACH STATEMENT EXECUTE FUNCTION iam_audit_append_only();

CREATE TRIGGER iam_security_resource_event AFTER INSERT ON cap_outbox FOR EACH ROW EXECUTE FUNCTION iam_audit_resource_event();

CREATE TRIGGER iam_security_session AFTER INSERT OR DELETE OR UPDATE OF csrf_token ON iam_sessions FOR EACH ROW EXECUTE FUNCTION iam_audit_session();

CREATE TRIGGER iam_tenant_runtime_status_changed AFTER UPDATE OF status ON iam_tenants FOR EACH ROW WHEN (((old.status = 'active'::text) AND (new.status = 'inactive'::text))) EXECUTE FUNCTION iam_mark_tenant_revocation();

CREATE TRIGGER iam_tenant_status_changed AFTER UPDATE ON iam_tenants FOR EACH ROW WHEN ((old.status IS DISTINCT FROM new.status)) EXECUTE FUNCTION iam_revoke_subject_access();

CREATE CONSTRAINT TRIGGER mcp_account_changed_trigger AFTER UPDATE ON iam_accounts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION mcp_account_changed();

CREATE TRIGGER mcp_tenant_membership_changed_trigger AFTER UPDATE OF status ON iam_tenant_memberships FOR EACH ROW WHEN (((old.status = 'active'::text) AND (new.status <> 'active'::text))) EXECUTE FUNCTION mcp_tenant_membership_changed();

CREATE TRIGGER runtime_ai_event AFTER INSERT OR UPDATE OF status ON ai_runs FOR EACH ROW EXECUTE FUNCTION runtime_capture_ai_event();

CREATE CONSTRAINT TRIGGER runtime_definition_pointers AFTER INSERT OR UPDATE ON runtime_application_heads DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION runtime_definition_pointer_check();

CREATE TRIGGER runtime_project_scope AFTER INSERT OR DELETE ON project_members FOR EACH ROW EXECUTE FUNCTION runtime_project_scope_changed();

CREATE TRIGGER runtime_snapshot_immutable BEFORE DELETE OR UPDATE ON runtime_deployment_snapshots FOR EACH ROW EXECUTE FUNCTION runtime_immutable_definition();

CREATE TRIGGER runtime_tool_event AFTER INSERT OR UPDATE OF status ON cap_invocations FOR EACH ROW EXECUTE FUNCTION runtime_capture_tool_event();

CREATE TRIGGER runtime_version_immutable BEFORE DELETE OR UPDATE ON runtime_application_versions FOR EACH ROW EXECUTE FUNCTION runtime_immutable_definition();

ALTER TABLE ai_run_events
    ADD CONSTRAINT ai_run_events_tenant_id_run_id_fkey FOREIGN KEY (tenant_id, run_id) REFERENCES ai_runs(tenant_id, id);

ALTER TABLE ai_runs
    ADD CONSTRAINT ai_runs_identity_binding_id_fkey FOREIGN KEY (identity_binding_id) REFERENCES iam_identity_bindings(id);

ALTER TABLE automation_runs
    ADD CONSTRAINT automation_runs_tenant_id_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES automation_tasks(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE automation_tasks
    ADD CONSTRAINT automation_task_binding_fk FOREIGN KEY (identity_binding_id) REFERENCES iam_identity_bindings(id);

ALTER TABLE automation_tasks
    ADD CONSTRAINT automation_tasks_owner_fkey FOREIGN KEY (tenant_id, owner_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE cap_approvals
    ADD CONSTRAINT cap_approvals_identity_binding_id_fkey FOREIGN KEY (identity_binding_id) REFERENCES iam_identity_bindings(id);

ALTER TABLE cap_events
    ADD CONSTRAINT cap_events_identity_binding_id_fkey FOREIGN KEY (identity_binding_id) REFERENCES iam_identity_bindings(id);

ALTER TABLE cap_execution_bindings
    ADD CONSTRAINT cap_execution_bindings_plan_tenant_id_capability_capabilit_fkey FOREIGN KEY (plan_tenant_id, capability, capability_version, plan_id, plan_version) REFERENCES cap_execution_plans(tenant_id, capability, capability_version, plan_id, version);

ALTER TABLE cap_execution_records
    ADD CONSTRAINT cap_execution_records_tenant_id_invocation_id_fkey FOREIGN KEY (tenant_id, invocation_id) REFERENCES cap_invocations(tenant_id, invocation_id);

ALTER TABLE cap_invocations
    ADD CONSTRAINT cap_invocations_identity_binding_id_fkey FOREIGN KEY (identity_binding_id) REFERENCES iam_identity_bindings(id);

ALTER TABLE cap_outbox
    ADD CONSTRAINT cap_outbox_identity_binding_id_fkey FOREIGN KEY (identity_binding_id) REFERENCES iam_identity_bindings(id);

ALTER TABLE drive_files
    ADD CONSTRAINT drive_files_tenant_id_created_by_fkey FOREIGN KEY (tenant_id, created_by) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE drive_files
    ADD CONSTRAINT drive_files_tenant_id_file_id_fkey FOREIGN KEY (tenant_id, file_id) REFERENCES gov_files(tenant_id, id);

ALTER TABLE drive_files
    ADD CONSTRAINT drive_files_tenant_id_space_id_fkey FOREIGN KEY (tenant_id, space_id) REFERENCES drive_spaces(tenant_id, id);

ALTER TABLE drive_files
    ADD CONSTRAINT drive_files_tenant_id_space_id_folder_id_fkey FOREIGN KEY (tenant_id, space_id, folder_id) REFERENCES drive_folders(tenant_id, space_id, id);

ALTER TABLE drive_files
    ADD CONSTRAINT drive_files_tenant_id_space_id_trash_batch_id_fkey FOREIGN KEY (tenant_id, space_id, trash_batch_id) REFERENCES drive_trash_batches(tenant_id, space_id, id);

ALTER TABLE drive_folders
    ADD CONSTRAINT drive_folders_tenant_id_created_by_fkey FOREIGN KEY (tenant_id, created_by) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE drive_folders
    ADD CONSTRAINT drive_folders_tenant_id_space_id_fkey FOREIGN KEY (tenant_id, space_id) REFERENCES drive_spaces(tenant_id, id);

ALTER TABLE drive_folders
    ADD CONSTRAINT drive_folders_tenant_id_space_id_parent_id_fkey FOREIGN KEY (tenant_id, space_id, parent_id) REFERENCES drive_folders(tenant_id, space_id, id);

ALTER TABLE drive_folders
    ADD CONSTRAINT drive_folders_tenant_id_space_id_trash_batch_id_fkey FOREIGN KEY (tenant_id, space_id, trash_batch_id) REFERENCES drive_trash_batches(tenant_id, space_id, id);

ALTER TABLE drive_members
    ADD CONSTRAINT drive_members_tenant_id_account_id_fkey FOREIGN KEY (tenant_id, account_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE drive_members
    ADD CONSTRAINT drive_members_tenant_id_department_id_fkey FOREIGN KEY (tenant_id, department_id) REFERENCES identity_departments(tenant_id, id);

ALTER TABLE drive_members
    ADD CONSTRAINT drive_members_tenant_id_space_id_fkey FOREIGN KEY (tenant_id, space_id) REFERENCES drive_spaces(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE drive_spaces
    ADD CONSTRAINT drive_spaces_tenant_id_owner_id_fkey FOREIGN KEY (tenant_id, owner_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE drive_trash_batches
    ADD CONSTRAINT drive_trash_batches_tenant_id_deleted_by_fkey FOREIGN KEY (tenant_id, deleted_by) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE drive_trash_batches
    ADD CONSTRAINT drive_trash_batches_tenant_id_space_id_fkey FOREIGN KEY (tenant_id, space_id) REFERENCES drive_spaces(tenant_id, id);

ALTER TABLE drive_upload_chunks
    ADD CONSTRAINT drive_upload_chunks_tenant_id_upload_id_fkey FOREIGN KEY (tenant_id, upload_id) REFERENCES drive_uploads(tenant_id, id);

ALTER TABLE drive_uploads
    ADD CONSTRAINT drive_uploads_tenant_id_file_id_fkey FOREIGN KEY (tenant_id, file_id) REFERENCES gov_files(tenant_id, id);

ALTER TABLE drive_uploads
    ADD CONSTRAINT drive_uploads_tenant_id_owner_id_fkey FOREIGN KEY (tenant_id, owner_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE drive_uploads
    ADD CONSTRAINT drive_uploads_tenant_id_space_id_fkey FOREIGN KEY (tenant_id, space_id) REFERENCES drive_spaces(tenant_id, id);

ALTER TABLE drive_uploads
    ADD CONSTRAINT drive_uploads_tenant_id_space_id_folder_id_fkey FOREIGN KEY (tenant_id, space_id, folder_id) REFERENCES drive_folders(tenant_id, space_id, id);

ALTER TABLE drive_uploads
    ADD CONSTRAINT drive_uploads_tenant_id_storage_profile_id_fkey FOREIGN KEY (tenant_id, storage_profile_id) REFERENCES gov_storage_profiles(tenant_id, id);

ALTER TABLE example_orders
    ADD CONSTRAINT example_orders_tenant_id_supplier_id_fkey FOREIGN KEY (tenant_id, supplier_id) REFERENCES example_records(tenant_id, id);

ALTER TABLE cap_execution_records
    ADD CONSTRAINT execution_record_run FOREIGN KEY (tenant_id, run_id) REFERENCES ai_runs(tenant_id, id);

ALTER TABLE gov_announcement_files
    ADD CONSTRAINT gov_announcement_files_tenant_id_announcement_id_fkey FOREIGN KEY (tenant_id, announcement_id) REFERENCES gov_announcements(tenant_id, id);

ALTER TABLE gov_announcement_files
    ADD CONSTRAINT gov_announcement_files_tenant_id_file_id_fkey FOREIGN KEY (tenant_id, file_id) REFERENCES gov_files(tenant_id, id);

ALTER TABLE gov_announcements
    ADD CONSTRAINT gov_announcements_tenant_id_department_id_fkey FOREIGN KEY (tenant_id, department_id) REFERENCES identity_departments(tenant_id, id);

ALTER TABLE gov_announcements
    ADD CONSTRAINT gov_announcements_tenant_id_sender_id_fkey FOREIGN KEY (tenant_id, sender_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE gov_audit
    ADD CONSTRAINT gov_audit_tenant_id_actor_id_fkey FOREIGN KEY (tenant_id, actor_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE gov_audit
    ADD CONSTRAINT gov_audit_tenant_id_scope_key_fkey FOREIGN KEY (tenant_id, scope_key) REFERENCES gov_scopes(tenant_id, scope_key) ON DELETE CASCADE;

ALTER TABLE gov_deliveries
    ADD CONSTRAINT gov_deliveries_tenant_id_message_id_fkey FOREIGN KEY (tenant_id, message_id) REFERENCES gov_notifications(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE gov_dictionaries
    ADD CONSTRAINT gov_dictionaries_tenant_id_scope_key_fkey FOREIGN KEY (tenant_id, scope_key) REFERENCES gov_scopes(tenant_id, scope_key);

ALTER TABLE gov_dictionary_items
    ADD CONSTRAINT gov_dictionary_items_tenant_id_dictionary_id_fkey FOREIGN KEY (tenant_id, dictionary_id) REFERENCES gov_dictionaries(tenant_id, id);

ALTER TABLE gov_dictionary_items
    ADD CONSTRAINT gov_dictionary_items_tenant_id_scope_key_fkey FOREIGN KEY (tenant_id, scope_key) REFERENCES gov_scopes(tenant_id, scope_key);

ALTER TABLE gov_dictionary_overrides
    ADD CONSTRAINT gov_dictionary_overrides_tenant_id_dictionary_id_item_id_fkey FOREIGN KEY (tenant_id, dictionary_id, item_id) REFERENCES gov_dictionary_items(tenant_id, dictionary_id, id);

ALTER TABLE gov_dictionary_overrides
    ADD CONSTRAINT gov_dictionary_overrides_tenant_id_scope_key_fkey FOREIGN KEY (tenant_id, scope_key) REFERENCES gov_scopes(tenant_id, scope_key);

ALTER TABLE gov_dictionary_references
    ADD CONSTRAINT gov_dictionary_references_tenant_id_dictionary_id_item_id_fkey FOREIGN KEY (tenant_id, dictionary_id, item_id) REFERENCES gov_dictionary_items(tenant_id, dictionary_id, id);

ALTER TABLE gov_drafts
    ADD CONSTRAINT gov_drafts_tenant_id_actor_id_fkey FOREIGN KEY (tenant_id, actor_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE gov_drafts
    ADD CONSTRAINT gov_drafts_tenant_id_scope_key_fkey FOREIGN KEY (tenant_id, scope_key) REFERENCES gov_scopes(tenant_id, scope_key) ON DELETE CASCADE;

ALTER TABLE gov_events
    ADD CONSTRAINT gov_events_tenant_id_request_id_fkey FOREIGN KEY (tenant_id, request_id) REFERENCES iam_temporary_requests(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE gov_file_bindings
    ADD CONSTRAINT gov_file_bindings_tenant_id_file_id_fkey FOREIGN KEY (tenant_id, file_id) REFERENCES gov_files(tenant_id, id);

ALTER TABLE gov_file_bindings
    ADD CONSTRAINT gov_file_bindings_tenant_id_scope_key_fkey FOREIGN KEY (tenant_id, scope_key) REFERENCES gov_scopes(tenant_id, scope_key);

ALTER TABLE gov_files
    ADD CONSTRAINT gov_files_tenant_id_department_id_organization_id_fkey FOREIGN KEY (tenant_id, department_id, organization_id) REFERENCES identity_departments(tenant_id, id, organization_id);

ALTER TABLE gov_files
    ADD CONSTRAINT gov_files_tenant_id_owner_id_fkey FOREIGN KEY (tenant_id, owner_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE gov_files
    ADD CONSTRAINT gov_files_tenant_id_storage_profile_id_fkey FOREIGN KEY (tenant_id, storage_profile_id) REFERENCES gov_storage_profiles(tenant_id, id);

ALTER TABLE gov_notifications
    ADD CONSTRAINT gov_notifications_tenant_id_announcement_id_fkey FOREIGN KEY (tenant_id, announcement_id) REFERENCES gov_announcements(tenant_id, id);

ALTER TABLE gov_notifications
    ADD CONSTRAINT gov_notifications_tenant_id_department_id_fkey FOREIGN KEY (tenant_id, department_id) REFERENCES identity_departments(tenant_id, id);

ALTER TABLE gov_notifications
    ADD CONSTRAINT gov_notifications_tenant_id_event_id_fkey FOREIGN KEY (tenant_id, event_id) REFERENCES gov_events(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE gov_notifications
    ADD CONSTRAINT gov_notifications_tenant_id_recipient_id_fkey FOREIGN KEY (tenant_id, recipient_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE iam_reauthentication_proofs
    ADD CONSTRAINT gov_reauth_session_hash_fkey FOREIGN KEY (session_hash) REFERENCES iam_sessions(token_hash) ON DELETE CASCADE;

ALTER TABLE gov_revisions
    ADD CONSTRAINT gov_revisions_tenant_id_scope_key_fkey FOREIGN KEY (tenant_id, scope_key) REFERENCES gov_scopes(tenant_id, scope_key) ON DELETE CASCADE;

ALTER TABLE gov_scopes
    ADD CONSTRAINT gov_scopes_tenant_id_account_id_fkey FOREIGN KEY (tenant_id, account_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE gov_scopes
    ADD CONSTRAINT gov_scopes_tenant_id_department_id_fkey FOREIGN KEY (tenant_id, department_id) REFERENCES identity_departments(tenant_id, id);

ALTER TABLE gov_scopes
    ADD CONSTRAINT gov_scopes_tenant_id_organization_id_fkey FOREIGN KEY (tenant_id, organization_id) REFERENCES identity_organizations(tenant_id, id);

ALTER TABLE gov_settings_schedules
    ADD CONSTRAINT gov_settings_schedules_tenant_id_actor_id_fkey FOREIGN KEY (tenant_id, actor_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE gov_settings_schedules
    ADD CONSTRAINT gov_settings_schedules_tenant_id_department_id_fkey FOREIGN KEY (tenant_id, department_id) REFERENCES identity_departments(tenant_id, id);

ALTER TABLE gov_settings_schedules
    ADD CONSTRAINT gov_settings_schedules_tenant_id_draft_id_fkey FOREIGN KEY (tenant_id, draft_id) REFERENCES gov_drafts(tenant_id, id);

ALTER TABLE gov_settings_schedules
    ADD CONSTRAINT gov_settings_schedules_tenant_id_version_id_fkey FOREIGN KEY (tenant_id, version_id) REFERENCES gov_versions(tenant_id, id);

ALTER TABLE gov_values
    ADD CONSTRAINT gov_values_tenant_id_scope_key_fkey FOREIGN KEY (tenant_id, scope_key) REFERENCES gov_scopes(tenant_id, scope_key) ON DELETE CASCADE;

ALTER TABLE gov_versions
    ADD CONSTRAINT gov_versions_tenant_id_actor_id_fkey FOREIGN KEY (tenant_id, actor_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE gov_versions
    ADD CONSTRAINT gov_versions_tenant_id_scope_key_fkey FOREIGN KEY (tenant_id, scope_key) REFERENCES gov_scopes(tenant_id, scope_key) ON DELETE CASCADE;

ALTER TABLE iam_access_reviews
    ADD CONSTRAINT iam_access_reviews_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES iam_principals(id);

ALTER TABLE iam_access_reviews
    ADD CONSTRAINT iam_access_reviews_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id);

ALTER TABLE iam_account_roles
    ADD CONSTRAINT iam_account_roles_tenant_id_account_id_fkey FOREIGN KEY (tenant_id, account_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE iam_account_roles
    ADD CONSTRAINT iam_account_roles_tenant_id_role_id_fkey FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles(tenant_id, id) ON DELETE RESTRICT;

ALTER TABLE iam_accounts
    ADD CONSTRAINT iam_accounts_login_account_fkey FOREIGN KEY (login_account_id) REFERENCES iam_login_accounts(id);

ALTER TABLE iam_accounts
    ADD CONSTRAINT iam_accounts_machine_principal_id_fkey FOREIGN KEY (machine_principal_id) REFERENCES iam_principals(id);

ALTER TABLE iam_agent_delegations
    ADD CONSTRAINT iam_agent_delegations_tenant_id_agent_account_id_fkey FOREIGN KEY (tenant_id, agent_account_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE iam_agent_delegations
    ADD CONSTRAINT iam_agent_delegations_tenant_id_delegator_account_id_fkey FOREIGN KEY (tenant_id, delegator_account_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE iam_audit_export_permits
    ADD CONSTRAINT iam_audit_export_permits_membership_id_fkey FOREIGN KEY (membership_id) REFERENCES iam_tenant_memberships(id);

ALTER TABLE iam_audit_export_permits
    ADD CONSTRAINT iam_audit_export_permits_tenant_id_account_id_fkey FOREIGN KEY (tenant_id, account_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE iam_audit_export_permits
    ADD CONSTRAINT iam_audit_export_permits_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id);

ALTER TABLE iam_contact_challenges
    ADD CONSTRAINT iam_contact_challenges_login_account_id_fkey FOREIGN KEY (login_account_id) REFERENCES iam_login_accounts(id) ON DELETE CASCADE;

ALTER TABLE iam_context_role_grants
    ADD CONSTRAINT iam_context_role_grants_tenant_id_principal_id_fkey FOREIGN KEY (tenant_id, principal_id) REFERENCES iam_tenant_memberships(tenant_id, principal_id);

ALTER TABLE iam_context_role_grants
    ADD CONSTRAINT iam_context_role_grants_tenant_id_principal_id_identity_bi_fkey FOREIGN KEY (tenant_id, principal_id, identity_binding_id) REFERENCES iam_identity_bindings(tenant_id, principal_id, id);

ALTER TABLE iam_context_role_grants
    ADD CONSTRAINT iam_context_role_grants_tenant_id_role_id_fkey FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles(tenant_id, id);

ALTER TABLE identity_department_membership_roles
    ADD CONSTRAINT iam_department_membership_rol_tenant_id_account_id_departm_fkey FOREIGN KEY (tenant_id, account_id, department_id) REFERENCES identity_department_memberships(tenant_id, account_id, department_id) ON DELETE CASCADE;

ALTER TABLE identity_department_membership_roles
    ADD CONSTRAINT identity_department_membership_roles_tenant_id_role_id_fkey FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles(tenant_id, id) ON DELETE RESTRICT;

ALTER TABLE identity_department_memberships
    ADD CONSTRAINT identity_department_memberships_tenant_id_account_id_fkey FOREIGN KEY (tenant_id, account_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE identity_department_memberships
    ADD CONSTRAINT identity_department_memberships_tenant_id_department_id_organiz FOREIGN KEY (tenant_id, department_id, organization_id) REFERENCES identity_departments(tenant_id, id, organization_id);

ALTER TABLE identity_departments
    ADD CONSTRAINT identity_departments_tenant_id_organization_id_fkey FOREIGN KEY (tenant_id, organization_id) REFERENCES identity_organizations(tenant_id, id);

ALTER TABLE identity_departments
    ADD CONSTRAINT identity_departments_tenant_id_parent_id_organization_id_fkey FOREIGN KEY (tenant_id, parent_id, organization_id) REFERENCES identity_departments(tenant_id, id, organization_id);

ALTER TABLE iam_disabled_resources
    ADD CONSTRAINT iam_disabled_resources_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id);

ALTER TABLE iam_external_identities
    ADD CONSTRAINT iam_external_identities_login_account_id_fkey FOREIGN KEY (login_account_id) REFERENCES iam_login_accounts(id);

ALTER TABLE iam_external_identity_reviews
    ADD CONSTRAINT iam_external_identity_reviews_decided_by_fkey FOREIGN KEY (decided_by) REFERENCES iam_login_accounts(id);

ALTER TABLE iam_external_identity_reviews
    ADD CONSTRAINT iam_external_identity_reviews_external_identity_id_fkey FOREIGN KEY (external_identity_id) REFERENCES iam_external_identities(id);

ALTER TABLE iam_external_identity_reviews
    ADD CONSTRAINT iam_external_identity_reviews_original_login_id_fkey FOREIGN KEY (original_login_id) REFERENCES iam_login_accounts(id);

ALTER TABLE iam_external_identity_reviews
    ADD CONSTRAINT iam_external_identity_reviews_requested_login_id_fkey FOREIGN KEY (requested_login_id) REFERENCES iam_login_accounts(id);

ALTER TABLE iam_external_identity_reviews
    ADD CONSTRAINT iam_external_identity_reviews_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id);

ALTER TABLE iam_identity_bindings
    ADD CONSTRAINT iam_identity_bindings_tenant_id_principal_id_fkey FOREIGN KEY (tenant_id, principal_id) REFERENCES iam_tenant_memberships(tenant_id, principal_id);

ALTER TABLE iam_login_accounts
    ADD CONSTRAINT iam_login_accounts_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES iam_principals(id);

ALTER TABLE iam_login_associations
    ADD CONSTRAINT iam_login_associations_associated_login_id_fkey FOREIGN KEY (associated_login_id) REFERENCES iam_login_accounts(id);

ALTER TABLE iam_login_associations
    ADD CONSTRAINT iam_login_associations_original_principal_id_fkey FOREIGN KEY (original_principal_id) REFERENCES iam_principals(id);

ALTER TABLE iam_login_associations
    ADD CONSTRAINT iam_login_associations_primary_login_id_fkey FOREIGN KEY (primary_login_id) REFERENCES iam_login_accounts(id);

ALTER TABLE iam_login_associations
    ADD CONSTRAINT iam_login_associations_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES iam_principals(id);

ALTER TABLE iam_login_merge_reviews
    ADD CONSTRAINT iam_login_merge_reviews_association_id_fkey FOREIGN KEY (association_id) REFERENCES iam_login_associations(id);

ALTER TABLE iam_login_merge_reviews
    ADD CONSTRAINT iam_login_merge_reviews_primary_login_id_fkey FOREIGN KEY (primary_login_id) REFERENCES iam_login_accounts(id);

ALTER TABLE iam_login_merge_reviews
    ADD CONSTRAINT iam_login_merge_reviews_primary_principal_id_fkey FOREIGN KEY (primary_principal_id) REFERENCES iam_principals(id);

ALTER TABLE iam_login_merge_reviews
    ADD CONSTRAINT iam_login_merge_reviews_secondary_login_id_fkey FOREIGN KEY (secondary_login_id) REFERENCES iam_login_accounts(id);

ALTER TABLE iam_login_merge_reviews
    ADD CONSTRAINT iam_login_merge_reviews_secondary_principal_id_fkey FOREIGN KEY (secondary_principal_id) REFERENCES iam_principals(id);

ALTER TABLE iam_login_merge_reviews
    ADD CONSTRAINT iam_login_merge_reviews_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id);

ALTER TABLE iam_login_sessions
    ADD CONSTRAINT iam_login_sessions_external_identity_id_fkey FOREIGN KEY (external_identity_id) REFERENCES iam_external_identities(id);

ALTER TABLE iam_login_sessions
    ADD CONSTRAINT iam_login_sessions_login_account_id_fkey FOREIGN KEY (login_account_id) REFERENCES iam_login_accounts(id) ON DELETE CASCADE;

ALTER TABLE iam_machine_credentials
    ADD CONSTRAINT iam_machine_credentials_tenant_id_account_id_fkey FOREIGN KEY (tenant_id, account_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE iam_membership_invitations
    ADD CONSTRAINT iam_membership_invitations_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id);

ALTER TABLE iam_membership_invitations
    ADD CONSTRAINT iam_membership_invitations_tenant_id_inviter_account_id_fkey FOREIGN KEY (tenant_id, inviter_account_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE identity_organizations
    ADD CONSTRAINT identity_organizations_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id);

ALTER TABLE iam_password_credentials
    ADD CONSTRAINT iam_password_credentials_login_account_id_fkey FOREIGN KEY (login_account_id) REFERENCES iam_login_accounts(id) ON DELETE CASCADE;

ALTER TABLE iam_pending_registrations
    ADD CONSTRAINT iam_pending_registrations_login_account_id_fkey FOREIGN KEY (login_account_id) REFERENCES iam_login_accounts(id) ON DELETE CASCADE;

ALTER TABLE iam_pending_registrations
    ADD CONSTRAINT iam_pending_registrations_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id);

ALTER TABLE iam_policy_heads
    ADD CONSTRAINT iam_policy_heads_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id);

ALTER TABLE iam_policy_heads
    ADD CONSTRAINT iam_policy_heads_tenant_id_version_id_fkey FOREIGN KEY (tenant_id, version_id) REFERENCES iam_policy_versions(tenant_id, id);

ALTER TABLE iam_policy_versions
    ADD CONSTRAINT iam_policy_versions_author_principal_id_fkey FOREIGN KEY (author_principal_id) REFERENCES iam_principals(id);

ALTER TABLE iam_policy_versions
    ADD CONSTRAINT iam_policy_versions_reviewer_principal_id_fkey FOREIGN KEY (reviewer_principal_id) REFERENCES iam_principals(id);

ALTER TABLE iam_policy_versions
    ADD CONSTRAINT iam_policy_versions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id);

ALTER TABLE iam_principals
    ADD CONSTRAINT iam_principals_created_in_tenant_fkey FOREIGN KEY (created_in_tenant) REFERENCES iam_tenants(id);

ALTER TABLE iam_recovery_codes
    ADD CONSTRAINT iam_recovery_codes_login_account_id_fkey FOREIGN KEY (login_account_id) REFERENCES iam_totp_credentials(login_account_id) ON DELETE CASCADE;

ALTER TABLE iam_security_retention_receipts
    ADD CONSTRAINT iam_security_retention_receipts_checkpoint_id_fkey FOREIGN KEY (checkpoint_id) REFERENCES iam_security_checkpoints(last_id);

ALTER TABLE iam_sessions
    ADD CONSTRAINT iam_session_identity_binding_fkey FOREIGN KEY (identity_binding_id) REFERENCES iam_identity_bindings(id);

ALTER TABLE iam_sessions
    ADD CONSTRAINT iam_sessions_external_identity_id_fkey FOREIGN KEY (external_identity_id) REFERENCES iam_external_identities(id);

ALTER TABLE iam_sessions
    ADD CONSTRAINT iam_sessions_tenant_id_account_id_fkey FOREIGN KEY (tenant_id, account_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE iam_temporary_requests
    ADD CONSTRAINT iam_temporary_requests_tenant_id_applicant_id_fkey FOREIGN KEY (tenant_id, applicant_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE iam_temporary_requests
    ADD CONSTRAINT iam_temporary_requests_tenant_id_reviewer_id_fkey FOREIGN KEY (tenant_id, reviewer_id) REFERENCES iam_accounts(tenant_id, id);

ALTER TABLE iam_temporary_requests
    ADD CONSTRAINT iam_temporary_requests_tenant_id_revoked_by_fkey FOREIGN KEY (tenant_id, revoked_by) REFERENCES iam_accounts(tenant_id, id);

ALTER TABLE iam_tenant_memberships
    ADD CONSTRAINT iam_tenant_memberships_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES iam_principals(id);

ALTER TABLE iam_tenant_memberships
    ADD CONSTRAINT iam_tenant_memberships_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id);

ALTER TABLE iam_totp_credentials
    ADD CONSTRAINT iam_totp_credentials_login_account_id_fkey FOREIGN KEY (login_account_id) REFERENCES iam_login_accounts(id) ON DELETE CASCADE;

ALTER TABLE iam_verified_contacts
    ADD CONSTRAINT iam_verified_contacts_login_account_id_fkey FOREIGN KEY (login_account_id) REFERENCES iam_login_accounts(id) ON DELETE CASCADE;

ALTER TABLE identity_account_profiles
    ADD CONSTRAINT identity_account_profiles_tenant_id_account_id_fkey FOREIGN KEY (tenant_id, account_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE kb_chunks
    ADD CONSTRAINT kb_chunks_tenant_id_document_id_fkey FOREIGN KEY (tenant_id, document_id) REFERENCES kb_documents(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE mail_channels
    ADD CONSTRAINT mail_channels_tenant_id_credential_id_fkey FOREIGN KEY (tenant_id, credential_id) REFERENCES mail_credentials(tenant_id, id);

ALTER TABLE mail_channels
    ADD CONSTRAINT mail_channels_tenant_id_mailbox_id_fkey FOREIGN KEY (tenant_id, mailbox_id) REFERENCES mail_mailboxes(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE mail_credentials
    ADD CONSTRAINT mail_credentials_tenant_id_mailbox_id_fkey FOREIGN KEY (tenant_id, mailbox_id) REFERENCES mail_mailboxes(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE mail_inbox_syncs
    ADD CONSTRAINT mail_inbox_syncs_tenant_id_mailbox_id_fkey FOREIGN KEY (tenant_id, mailbox_id) REFERENCES mail_mailboxes(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE mail_mailboxes
    ADD CONSTRAINT mail_mailboxes_owner_fkey FOREIGN KEY (tenant_id, owner_account_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE mail_message_attachments
    ADD CONSTRAINT mail_message_attachments_tenant_id_file_id_fkey FOREIGN KEY (tenant_id, file_id) REFERENCES gov_files(tenant_id, id);

ALTER TABLE mail_message_attachments
    ADD CONSTRAINT mail_message_attachments_tenant_id_message_id_fkey FOREIGN KEY (tenant_id, message_id) REFERENCES mail_messages(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE mail_message_recipients
    ADD CONSTRAINT mail_message_recipients_tenant_id_message_id_fkey FOREIGN KEY (tenant_id, message_id) REFERENCES mail_messages(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE mail_messages
    ADD CONSTRAINT mail_messages_tenant_id_mailbox_id_fkey FOREIGN KEY (tenant_id, mailbox_id) REFERENCES mail_mailboxes(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE mail_messages
    ADD CONSTRAINT mail_messages_tenant_id_owner_account_id_fkey FOREIGN KEY (tenant_id, owner_account_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE mail_outbound_deliveries
    ADD CONSTRAINT mail_outbound_deliveries_tenant_id_mailbox_id_fkey FOREIGN KEY (tenant_id, mailbox_id) REFERENCES mail_mailboxes(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE mail_outbound_deliveries
    ADD CONSTRAINT mail_outbound_deliveries_tenant_id_message_id_fkey FOREIGN KEY (tenant_id, message_id) REFERENCES mail_messages(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE mail_outbound_deliveries
    ADD CONSTRAINT mail_outbound_deliveries_tenant_id_submitted_by_fkey FOREIGN KEY (tenant_id, submitted_by) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE mcp_call_audits
    ADD CONSTRAINT mcp_call_audits_tenant_id_account_id_fkey FOREIGN KEY (tenant_id, account_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE mcp_call_audits
    ADD CONSTRAINT mcp_call_audits_tenant_id_token_id_fkey FOREIGN KEY (tenant_id, token_id) REFERENCES mcp_tokens(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE mcp_token_tools
    ADD CONSTRAINT mcp_token_tools_token_id_fkey FOREIGN KEY (token_id) REFERENCES mcp_tokens(id) ON DELETE CASCADE;

ALTER TABLE mcp_tokens
    ADD CONSTRAINT mcp_tokens_identity_binding_id_fkey FOREIGN KEY (identity_binding_id) REFERENCES iam_identity_bindings(id);

ALTER TABLE mcp_tokens
    ADD CONSTRAINT mcp_tokens_tenant_id_account_id_fkey FOREIGN KEY (tenant_id, account_id) REFERENCES iam_accounts(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE mcp_tokens
    ADD CONSTRAINT mcp_tokens_tenant_id_revoked_by_fkey FOREIGN KEY (tenant_id, revoked_by) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE memory_corrections
    ADD CONSTRAINT memory_corrections_tenant_id_memory_id_fkey FOREIGN KEY (tenant_id, memory_id) REFERENCES memory_items(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE memory_relations
    ADD CONSTRAINT memory_relations_tenant_id_source_memory_id_fkey FOREIGN KEY (tenant_id, source_memory_id) REFERENCES memory_items(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE memory_relations
    ADD CONSTRAINT memory_relations_tenant_id_target_memory_id_fkey FOREIGN KEY (tenant_id, target_memory_id) REFERENCES memory_items(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE memory_sources
    ADD CONSTRAINT memory_sources_tenant_id_event_id_fkey FOREIGN KEY (tenant_id, event_id) REFERENCES memory_events(tenant_id, event_id);

ALTER TABLE memory_sources
    ADD CONSTRAINT memory_sources_tenant_id_memory_id_fkey FOREIGN KEY (tenant_id, memory_id) REFERENCES memory_items(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE orch_checkpoints
    ADD CONSTRAINT orch_checkpoints_tenant_id_run_id_fkey FOREIGN KEY (tenant_id, run_id) REFERENCES ai_runs(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE project_members
    ADD CONSTRAINT project_members_account_fkey FOREIGN KEY (tenant_id, account_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE project_members
    ADD CONSTRAINT project_members_project_fkey FOREIGN KEY (tenant_id, project_id) REFERENCES projects(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE project_plans
    ADD CONSTRAINT project_plans_owner_fkey FOREIGN KEY (tenant_id, owner_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE project_plans
    ADD CONSTRAINT project_plans_project_fkey FOREIGN KEY (tenant_id, project_id) REFERENCES projects(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE project_task_briefs
    ADD CONSTRAINT project_task_briefs_task_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES project_tasks(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE project_tasks
    ADD CONSTRAINT project_tasks_assignee_fkey FOREIGN KEY (tenant_id, assignee_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE project_tasks
    ADD CONSTRAINT project_tasks_plan_fkey FOREIGN KEY (tenant_id, plan_id) REFERENCES project_plans(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE project_tasks
    ADD CONSTRAINT project_tasks_project_fkey FOREIGN KEY (tenant_id, project_id) REFERENCES projects(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE projects
    ADD CONSTRAINT projects_owner_fkey FOREIGN KEY (tenant_id, owner_id) REFERENCES gov_identity_refs(tenant_id, account_id);

ALTER TABLE runtime_application_changes
    ADD CONSTRAINT runtime_application_changes_tenant_id_application_id_fkey FOREIGN KEY (tenant_id, application_id) REFERENCES runtime_application_heads(tenant_id, application_id);

ALTER TABLE runtime_application_heads
    ADD CONSTRAINT runtime_application_heads_base_digest_fkey FOREIGN KEY (base_digest) REFERENCES runtime_deployment_snapshots(digest);

ALTER TABLE runtime_application_heads
    ADD CONSTRAINT runtime_application_heads_tenant_id_plugin_id_fkey FOREIGN KEY (tenant_id, plugin_id) REFERENCES runtime_installations(tenant_id, plugin_id);

ALTER TABLE runtime_application_states
    ADD CONSTRAINT runtime_application_states_tenant_id_plugin_id_fkey FOREIGN KEY (tenant_id, plugin_id) REFERENCES runtime_installations(tenant_id, plugin_id);

ALTER TABLE runtime_application_versions
    ADD CONSTRAINT runtime_application_versions_base_digest_fkey FOREIGN KEY (base_digest) REFERENCES runtime_deployment_snapshots(digest);

ALTER TABLE runtime_application_versions
    ADD CONSTRAINT runtime_application_versions_tenant_id_agent_run_id_fkey FOREIGN KEY (tenant_id, agent_run_id) REFERENCES ai_runs(tenant_id, id);

ALTER TABLE runtime_application_versions
    ADD CONSTRAINT runtime_application_versions_tenant_id_application_id_fkey FOREIGN KEY (tenant_id, application_id) REFERENCES runtime_application_heads(tenant_id, application_id);

ALTER TABLE runtime_installation_history
    ADD CONSTRAINT runtime_installation_history_tenant_id_plugin_id_fkey FOREIGN KEY (tenant_id, plugin_id) REFERENCES runtime_installations(tenant_id, plugin_id);

ALTER TABLE runtime_invocation_dependencies
    ADD CONSTRAINT runtime_invocation_dependencies_tenant_id_invocation_id_fkey FOREIGN KEY (tenant_id, invocation_id) REFERENCES cap_invocations(tenant_id, invocation_id);

ALTER TABLE runtime_invocation_dependencies
    ADD CONSTRAINT runtime_invocation_dependencies_tenant_id_plugin_id_fkey FOREIGN KEY (tenant_id, plugin_id) REFERENCES runtime_installations(tenant_id, plugin_id);

ALTER TABLE runtime_run_dependencies
    ADD CONSTRAINT runtime_run_dependencies_tenant_id_plugin_id_fkey FOREIGN KEY (tenant_id, plugin_id) REFERENCES runtime_installations(tenant_id, plugin_id);

ALTER TABLE runtime_run_dependencies
    ADD CONSTRAINT runtime_run_dependencies_tenant_id_run_id_fkey FOREIGN KEY (tenant_id, run_id) REFERENCES ai_runs(tenant_id, id);

ALTER TABLE gov_settings_schedules
    ADD CONSTRAINT settings_schedule_binding_fk FOREIGN KEY (identity_binding_id) REFERENCES iam_identity_bindings(id);

CREATE FUNCTION identity_provision_tenant_organization() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    INSERT INTO identity_organizations (tenant_id, name, code, kind, status)
    VALUES (NEW.id, left(NEW.id, 100), 'organization', 'company', 'active');
    RETURN NEW;
END;
$$;

CREATE TRIGGER identity_provision_tenant_organization
    AFTER INSERT ON iam_tenants
    FOR EACH ROW EXECUTE FUNCTION identity_provision_tenant_organization();

-- Built-in runtime and event policy templates.
INSERT INTO runtime_installation_templates (plugin_id, version, application_id, enabled) VALUES
	('builtin:analytics', '1.0.1', 'analytics', true),
	('builtin:personal-preferences', '1.0.1', 'personal-preferences', true),
	('com.horizon.projects', '2.0.0', 'projects', true),
	('builtin:system-settings', '1.0.3', 'system-settings', true),
	('builtin:automation', '1.0.1', 'automation', true),
	('system.identity', '2.3.0', 'accounts', true),
	('builtin:mail', '1.0.0', 'mail', true),
	('builtin:capabilities', '1.1.0', 'capabilities', true),
	('builtin:account-security', '1.0.0', 'account-security', true),
	('builtin:iam-governance', '1.0.0', 'iam-governance', true),
	('builtin:agent-collaboration', '1.0.3', 'agent-collaboration', true);

INSERT INTO ai_trigger_policies
    (tenant_id, policy_id, version, enabled, event_type, agent, mode, instruction, max_depth) VALUES
	('*', 'example-record-review', 1, true, 'example.record.created', 'workspace_agent', 'async', 'A new example record was created. Review it: summarise what was created, check whether the title is ambiguous, and recommend one concrete next step. You may call read-only capabilities; do not modify business state.', 3),
	('*', 'project-task-brief', 1, true, 'project.task.assigned', 'task_briefer', 'async', 'A project task was just assigned. Read it with project_task_get (and project_plan_list when it has a plan), then store a short handover brief with project_task_brief_record: what the task is for, what the assignee needs first, and the main risk. Do not change any business field.', 5);

-- Seed the initial administrator. The default Argon2id hash is for the local
-- default credential. The bootstrap command can supply a custom hash.
DO $bootstrap$
DECLARE
    tenant text := COALESCE(NULLIF(current_setting('horizon.bootstrap_tenant', true), ''), 'workspace-default');
    login_name text := COALESCE(NULLIF(current_setting('horizon.bootstrap_username', true), ''), 'admin');
    credential_hash text := COALESCE(NULLIF(current_setting('horizon.bootstrap_password_hash', true), ''),
        '$argon2id$v=19$m=32768,t=2,p=1$wBWLvU0uQqTVqy6y8Rg+pA$zdAG2SGRlEEgxxhq0Ggw/QG9/wWPchV5X3Q0Tgh0Sjo');
    subject uuid;
    login_id uuid;
    account_id bigint;
    role_id bigint;
BEGIN
    IF current_setting('horizon.bootstrap_skip_administrator', true) = 'on' THEN
        RETURN;
    END IF;

    INSERT INTO iam_tenants (id) VALUES (tenant);
    INSERT INTO iam_principals (kind) VALUES ('human') RETURNING id INTO subject;
    INSERT INTO iam_login_accounts (principal_id, username)
    VALUES (subject, login_name) RETURNING id INTO login_id;
    INSERT INTO iam_password_credentials (login_account_id, password_hash)
    VALUES (login_id, credential_hash);
    INSERT INTO iam_tenant_memberships (tenant_id, principal_id) VALUES (tenant, subject);
    INSERT INTO iam_accounts (tenant_id, username, status, is_admin, login_account_id)
    VALUES (tenant, login_name, 'active', true, login_id) RETURNING id INTO account_id;
    INSERT INTO iam_roles (tenant_id, name, code, status, description, template_key, grants)
    SELECT tenant, 'System administrator', 'system-admin', 'active', 'Full workspace administration', 'tenant-admin',
           jsonb_agg(jsonb_build_object('permission', permission, 'effect', 'allow', 'scope', 'ALL') ORDER BY permission)
    FROM (
        SELECT resource || ':' || action AS permission
        FROM unnest(ARRAY['accounts', 'departments', 'roles']) AS resource
        CROSS JOIN unnest(ARRAY['read', 'create', 'update', 'delete']) AS action
        UNION ALL SELECT 'organizations:read'
        UNION ALL SELECT 'organizations:update'
        UNION ALL SELECT 'access:manage'
        UNION ALL SELECT 'accounts:security'
    ) AS administrator_permissions
    RETURNING id INTO role_id;
    INSERT INTO iam_account_roles (tenant_id, account_id, role_id) VALUES (tenant, account_id, role_id);
    INSERT INTO identity_account_profiles (tenant_id, account_id, display_name) VALUES (tenant, account_id, login_name);

    INSERT INTO runtime_installations (tenant_id, plugin_id, version, status)
    SELECT tenant, plugin_id, version, CASE WHEN enabled THEN 'enabled' ELSE 'installed' END
    FROM runtime_installation_templates;
END
$bootstrap$;

-- Written last so a committed marker identifies a complete baseline.
CREATE TABLE horizon_database_initialized (
    initialized_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO horizon_database_initialized DEFAULT VALUES;
