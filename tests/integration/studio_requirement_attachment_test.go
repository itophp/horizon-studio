//go:build integration

package tests

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestStudioRequirementAttachments(t *testing.T) {
	handler, pool, account := studioFixture(t)
	owner := loginIAM(t, handler, "root-admin")
	root := t.TempDir()
	for _, name := range []string{"go.mod", "cmd/api/main.go"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	project := studioCall(t, owner, "studio.project.link", map[string]any{"path": root, "name": "Attachment project"})
	projectID := studioRequirementID(t, project)
	requirementID := studioRequirementID(t, studioCall(t, owner, "studio.requirement.create", map[string]any{
		"project": projectID, "title": "Attachment requirement",
	}))
	childID := studioRequirementID(t, studioCall(t, owner, "studio.requirement.create", map[string]any{
		"project": projectID, "parent": requirementID, "title": "Child requirement",
	}))

	content := base64.StdEncoding.EncodeToString([]byte("hello attachment"))
	file := studioCall(t, owner, "file.upload", map[string]any{
		"name": "note.txt", "purpose": "attachment", "content": content,
	})
	if file.Status != "succeeded" {
		t.Fatalf("file upload failed: %+v", file)
	}
	fileID := int64(file.Data.(map[string]any)["id"].(float64))

	added := studioCall(t, owner, "studio.requirement.attachment.add", map[string]any{
		"project": projectID, "requirement": requirementID, "file": fileID,
	})
	if added.Status != "succeeded" || added.Data.(map[string]any)["fileId"] != float64(fileID) || len(added.EventIds) != 1 {
		t.Fatalf("attachment bind did not commit: %+v", added)
	}
	listed := studioCall(t, owner, "studio.requirement.attachment.list", map[string]any{
		"project": projectID, "requirement": requirementID,
	})
	attachments := listed.Data.(map[string]any)["attachments"].([]any)
	if len(attachments) != 1 || attachments[0].(map[string]any)["fileId"] != float64(fileID) ||
		attachments[0].(map[string]any)["name"] != "note.txt" {
		t.Fatalf("attachment list lost the binding: %+v", listed.Data)
	}

	// A child requirement has its own attachment set.
	childFile := studioCall(t, owner, "file.upload", map[string]any{
		"name": "child.txt", "purpose": "attachment", "content": content,
	})
	childFileID := int64(childFile.Data.(map[string]any)["id"].(float64))
	studioCall(t, owner, "studio.requirement.attachment.add", map[string]any{
		"project": projectID, "requirement": childID, "file": childFileID,
	})
	childList := studioCall(t, owner, "studio.requirement.attachment.list", map[string]any{
		"project": projectID, "requirement": childID,
	})
	if len(childList.Data.(map[string]any)["attachments"].([]any)) != 1 {
		t.Fatalf("child attachment missing: %+v", childList.Data)
	}

	// The session context carries attachment metadata for the linked requirement.
	task := studioRequirementID(t, studioCall(t, owner, "studio.item.create", map[string]any{
		"project": projectID, "kind": "execution", "title": "Attachment task", "requirementId": requirementID,
	}))
	studioCall(t, owner, "studio.item.link_session", map[string]any{"id": task, "sessionId": "attachment-session"})
	context := studioCall(t, owner, "studio.session.context", map[string]any{"sessionId": "attachment-session"})
	requirement := context.Data.(map[string]any)["requirement"].(map[string]any)
	sessionAttachments := requirement["attachments"].([]any)
	if len(sessionAttachments) != 1 || sessionAttachments[0].(map[string]any)["fileId"] != float64(fileID) {
		t.Fatalf("session context lost attachment metadata: %+v", requirement)
	}

	// Removing the binding keeps the file readable in the shared file service.
	removed := studioCall(t, owner, "studio.requirement.attachment.remove", map[string]any{
		"project": projectID, "requirement": requirementID, "file": fileID,
	})
	if removed.Status != "succeeded" || len(removed.EventIds) != 1 {
		t.Fatalf("attachment unbind failed: %+v", removed)
	}
	listed = studioCall(t, owner, "studio.requirement.attachment.list", map[string]any{
		"project": projectID, "requirement": requirementID,
	})
	if len(listed.Data.(map[string]any)["attachments"].([]any)) != 0 {
		t.Fatalf("attachment binding survived removal: %+v", listed.Data)
	}
	fileContent := studioCall(t, owner, "file.content.read", map[string]any{"id": fileID})
	if fileContent.Status != "succeeded" || fileContent.Data.(map[string]any)["content"] != content {
		t.Fatalf("removing the binding deleted the file: %+v", fileContent)
	}

	// An archived requirement accepts no new bindings.
	studioCall(t, owner, "studio.requirement.archive", map[string]any{"project": projectID, "id": requirementID})
	archivedAdd := studioCall(t, owner, "studio.requirement.attachment.add", map[string]any{
		"project": projectID, "requirement": requirementID, "file": fileID,
	})
	if archivedAdd.ErrorCode != "conflict" {
		t.Fatalf("archived requirement accepted an attachment: %+v", archivedAdd)
	}

	// Attachment metadata is only exposed for the caller's own project.
	createStudioAccount(t, owner, pool, account.ID, "attachment-other", "studio:read")
	other := loginIAM(t, handler, "attachment-other")
	if blocked := studioCall(t, other, "studio.requirement.attachment.list", map[string]any{
		"project": projectID, "requirement": requirementID,
	}); blocked.ErrorCode != "not_found" {
		t.Fatalf("foreign account listed requirement attachments: %+v", blocked)
	}
}
