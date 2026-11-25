package studio

import (
	"context"
	"errors"
	"strconv"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen/studiodb"

	"github.com/jackc/pgx/v5"
)

const (
	AttachmentAddedEvent   = "studio.requirement.attachment.added"
	AttachmentRemovedEvent = "studio.requirement.attachment.removed"
)

func requirementAttachmentListDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.requirement.attachment.list", Version: "1.0.0", Title: "List requirement attachments",
		Description: "List the files bound to one requirement of a managed project.",
		Domain:      "studio", Entity: "studio_requirement_attachment",
		Entities:   []string{"studio_requirement_attachment", "studio_requirement", "file"},
		Permission: ReadPermission, Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true,
		Executor: "go:studio.requirement.attachment.list", Tags: []string{"studio", "read"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "requirement": contracts.Int(1, 1<<53),
		}, "project", "requirement"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"attachments": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"fileId": contracts.Int(1, 1<<40), "name": {Type: "string"}, "mime": {Type: "string"},
				"size": contracts.Int(0, 1<<50), "createdAt": {Type: "string"},
			}, "fileId", "name", "mime", "size", "createdAt")},
		}, "attachments"),
	}
}

func (m *Module) requirementAttachmentList(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, project, err := m.ownedProject(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(ReadPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	requirementID, ok := number(in["requirement"])
	if !ok || requirementID < 1 {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	attachments, err := m.requirementAttachments(ctx, p.TenantID, p.Account.ID, project.ID, int64(requirementID))
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: map[string]any{"attachments": attachments}, Summary: "attachments=" + itoa(len(attachments))}, nil
}

func requirementAttachmentAddDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.requirement.attachment.add", Version: "1.0.0", Title: "Bind a file to a requirement",
		SuccessEvents: []string{AttachmentAddedEvent},
		Description:   "Bind one uploaded attachment owned by the caller to a requirement of a managed project.",
		Domain:        "studio", Entity: "studio_requirement_attachment",
		Entities:   []string{"studio_requirement_attachment", "studio_requirement", "file"},
		Permission: WritePermission, Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.requirement.attachment.add", Tags: []string{"studio", "write"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "requirement": contracts.Int(1, 1<<53),
			"file": contracts.Int(1, 1<<40),
		}, "project", "requirement", "file"),
		Output:   contracts.Object(map[string]*contracts.Schema{"fileId": contracts.Int(1, 1<<40)}, "fileId"),
		Loggable: []string{"project", "requirement", "file"},
	}
}

func (m *Module) requirementAttachmentAdd(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(WritePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	projectID, ok := projectID(in)
	if !ok {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	requirementID, okRequirement := number(in["requirement"])
	fileID, okFile := number(in["file"])
	if !okRequirement || requirementID < 1 || !okFile || fileID < 1 {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	q := studiodb.New(tx)
	current, err := q.StudioRequirementGet(ctx, studiodb.StudioRequirementGetParams{
		TenantID: p.TenantID, ID: int64(requirementID), CreatedBy: p.Account.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	if current.ProjectID != projectID {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if current.Archived {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	if _, err := q.StudioOwnedAttachment(ctx, studiodb.StudioOwnedAttachmentParams{
		TenantID: p.TenantID, ID: int64(fileID), OwnerID: p.Account.ID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("invalid")
	} else if err != nil {
		return contracts.Output{}, err
	}
	row, err := q.StudioRequirementAttachmentAdd(ctx, studiodb.StudioRequirementAttachmentAddParams{
		TenantID: p.TenantID, RequirementID: int64(requirementID), ProjectID: projectID,
		FileID: int64(fileID), CreatedBy: p.Account.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{
		Data:    map[string]any{"fileId": row.FileID},
		Summary: "attachment=" + strconv.FormatInt(row.FileID, 10),
		Events: []contracts.Event{{
			Type: AttachmentAddedEvent, AggregateType: "studio_requirement",
			AggregateID: strconv.FormatInt(int64(requirementID), 10),
			Payload:     map[string]any{"fileId": row.FileID, "requirementId": int64(requirementID)},
		}},
	}, nil
}

func requirementAttachmentRemoveDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.requirement.attachment.remove", Version: "1.0.0", Title: "Unbind a requirement attachment",
		SuccessEvents: []string{AttachmentRemovedEvent},
		Description:   "Remove the binding between a requirement and one attachment. The file itself is kept.",
		Domain:        "studio", Entity: "studio_requirement_attachment",
		Entities:   []string{"studio_requirement_attachment", "studio_requirement", "file"},
		Permission: WritePermission, Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.requirement.attachment.remove", Tags: []string{"studio", "write"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "requirement": contracts.Int(1, 1<<53),
			"file": contracts.Int(1, 1<<40),
		}, "project", "requirement", "file"),
		Output:   contracts.Object(map[string]*contracts.Schema{"fileId": contracts.Int(1, 1<<40)}, "fileId"),
		Loggable: []string{"project", "requirement", "file"},
	}
}

func (m *Module) requirementAttachmentRemove(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(WritePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	projectID, ok := projectID(in)
	if !ok {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	requirementID, okRequirement := number(in["requirement"])
	fileID, okFile := number(in["file"])
	if !okRequirement || requirementID < 1 || !okFile || fileID < 1 {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	q := studiodb.New(tx)
	current, err := q.StudioRequirementGet(ctx, studiodb.StudioRequirementGetParams{
		TenantID: p.TenantID, ID: int64(requirementID), CreatedBy: p.Account.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	if current.ProjectID != projectID {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if current.Archived {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	removed, err := q.StudioRequirementAttachmentRemove(ctx, studiodb.StudioRequirementAttachmentRemoveParams{
		TenantID: p.TenantID, RequirementID: int64(requirementID), FileID: int64(fileID), CreatedBy: p.Account.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{
		Data:    map[string]any{"fileId": removed},
		Summary: "attachment=" + strconv.FormatInt(removed, 10),
		Events: []contracts.Event{{
			Type: AttachmentRemovedEvent, AggregateType: "studio_requirement",
			AggregateID: strconv.FormatInt(int64(requirementID), 10),
			Payload:     map[string]any{"fileId": removed, "requirementId": int64(requirementID)},
		}},
	}, nil
}

// requirementAttachments is shared by the list capability and the session
// context read so the Agent sees the same attachment projection as the page.
func (m *Module) requirementAttachments(ctx context.Context, tenant string, createdBy, projectID, requirementID int64) ([]map[string]any, error) {
	rows, err := studiodb.New(m.pool).StudioRequirementAttachmentList(ctx, studiodb.StudioRequirementAttachmentListParams{
		TenantID: tenant, RequirementID: requirementID, ProjectID: projectID, CreatedBy: createdBy,
	})
	if err != nil {
		return nil, err
	}
	attachments := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		attachments = append(attachments, map[string]any{
			"fileId": row.ID, "name": row.Name, "mime": row.Mime, "size": row.Size,
			"createdAt": instant(row.CreatedAt),
		})
	}
	return attachments, nil
}
