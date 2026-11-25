package identity

import genidentity "horizon/foundation/gen/identity"

type AccountInput struct {
	ID                                          int64
	Username, FullName, Email, Status, Password string
}

// RoleInput is the reviewed payload of role creation and update.
type RoleInput struct {
	Revision    string
	ID          int64
	Name        string
	Code        string
	Description string
	Status      string
	Grants      []*genidentity.PermissionGrant
}

// DeleteInput addresses one structure or account by primary key.
type DeleteInput struct{ ID int64 }
