package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
)

// projectGateway talks to a managed project's Capability Gateway on behalf of
// the Studio account. Credentials live only in process memory, keyed by
// tenant, account and project record, so two linked projects never share a
// login even when they point at the same origin. Every invocation carries the
// caller supplied idempotency key.
type projectGateway struct {
	mu       sync.Mutex
	sessions map[string]gatewaySession
	client   *http.Client
}

type gatewaySession struct {
	origin string
	cookie string
	csrf   string
}

func newProjectGateway() *projectGateway {
	return &projectGateway{sessions: map[string]gatewaySession{}, client: &http.Client{
		Timeout:       120 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func sessionKey(p *identity.Principal, projectID int64) string {
	return fmt.Sprintf("%s:%d:%d", p.TenantID, p.Account.ID, projectID)
}

func (g *projectGateway) session(key string) (gatewaySession, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	session, ok := g.sessions[key]
	if !ok || session.cookie == "" || session.csrf == "" {
		return gatewaySession{}, contracts.Problem("请先登录目标项目")
	}
	return session, nil
}

func (g *projectGateway) login(origin, username, password string) (gatewaySession, error) {
	body, _ := json.Marshal(map[string]any{"username": username, "password": password})
	request, err := http.NewRequest(http.MethodPost, origin+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		return gatewaySession{}, err
	}
	request.Header.Set("content-type", "application/json")
	request.Header.Set("origin", origin)
	response, err := g.client.Do(request)
	if err != nil {
		return gatewaySession{}, contracts.Problem("unavailable")
	}
	defer response.Body.Close()
	var payload struct {
		Profile struct {
			CSRFToken string `json:"csrfToken"`
		} `json:"profile"`
	}
	_ = json.NewDecoder(response.Body).Decode(&payload)
	cookie := ""
	for _, value := range response.Cookies() {
		if cookie != "" {
			cookie += "; "
		}
		cookie += value.Name + "=" + value.Value
	}
	if response.StatusCode != http.StatusOK || payload.Profile.CSRFToken == "" || cookie == "" {
		return gatewaySession{}, contracts.Problem("登录失败，请检查目标项目账号")
	}
	return gatewaySession{origin: origin, cookie: cookie, csrf: payload.Profile.CSRFToken}, nil
}

func (g *projectGateway) invoke(origin string, session gatewaySession, capability, version string, input map[string]any, key string) (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{"capability": capability, "version": version, "input": input, "idempotencyKey": key})
	request, err := http.NewRequest(http.MethodPost, origin+"/api/capabilities/"+url.PathEscape(capability)+"/invocations", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("content-type", "application/json")
	request.Header.Set("origin", origin)
	request.Header.Set("cookie", session.cookie)
	request.Header.Set("x-csrf-token", session.csrf)
	response, err := g.client.Do(request)
	if err != nil {
		return nil, contracts.Problem("unavailable")
	}
	defer response.Body.Close()
	var result map[string]any
	_ = json.NewDecoder(response.Body).Decode(&result)
	if response.StatusCode != http.StatusOK {
		if fields, _ := result["error"].(map[string]any); textOr(fields["message"]) != "" {
			return nil, contracts.Problem(textOr(fields["message"]))
		}
		return nil, contracts.Problem(fmt.Sprintf("能力调用失败（%d）", response.StatusCode))
	}
	return result, nil
}

func gatewayLoginDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.gateway.login", Version: "1.0.0", Title: "Log into a managed project",
		SuccessEvents: []string{"studio.gateway.session.opened"},
		Idempotency:   contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Description:   "Authenticate the workbench against a managed project's Capability Gateway. Credentials are held in process memory only and are never logged.",
		Domain:        "studio", Entity: "studio_project", Permission: InvokePermission,
		Risk: contracts.RiskMedium, Approval: contracts.ApprovalAuto,
		Executor: "go:studio.gateway.login", Tags: []string{"studio", "gateway"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "username": text(1, 200), "password": text(1, 200),
		}, "project", "username", "password"),
		Output: contracts.Object(map[string]*contracts.Schema{"ok": {Type: "boolean"}}, "ok"),
	}
}

var remoteGateway = newProjectGateway()

func (m *Module) gatewayLogin(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, project, err := m.ownedProject(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(InvokePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	origin, err := runningBackendOrigin(ctx, ref(project))
	if err != nil {
		return contracts.Output{}, err
	}
	session, err := remoteGateway.login(origin, stringField(in, "username"), stringField(in, "password"))
	if err != nil {
		return contracts.Output{}, err
	}
	remoteGateway.mu.Lock()
	remoteGateway.sessions[sessionKey(p, project.ID)] = session
	remoteGateway.mu.Unlock()
	return contracts.Output{Data: map[string]any{"ok": true}, Summary: "gateway.login=project " + itoa(int(project.ID)), Events: []contracts.Event{{Type: "studio.gateway.session.opened", AggregateType: "studio_project", AggregateID: strconv.FormatInt(project.ID, 10), Payload: map[string]any{}}}}, nil
}

func gatewayLogoutDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.gateway.logout", Version: "1.0.0", Title: "Log out of a managed project",
		SuccessEvents: []string{"studio.gateway.session.closed"},
		Idempotency:   contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Description:   "Drop the stored project session of the current account.",
		Domain:        "studio", Entity: "studio_project", Permission: InvokePermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		Executor: "go:studio.gateway.logout", Tags: []string{"studio", "gateway"},
		Input:  contracts.Object(map[string]*contracts.Schema{"project": contracts.Int(1, 1<<53)}, "project"),
		Output: contracts.Object(map[string]*contracts.Schema{"ok": {Type: "boolean"}}, "ok"),
	}
}

func (m *Module) gatewayLogout(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, project, err := m.ownedProject(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(InvokePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	remoteGateway.mu.Lock()
	delete(remoteGateway.sessions, sessionKey(p, project.ID))
	remoteGateway.mu.Unlock()
	return contracts.Output{Data: map[string]any{"ok": true}, Events: []contracts.Event{{Type: "studio.gateway.session.closed", AggregateType: "studio_project", AggregateID: strconv.FormatInt(project.ID, 10), Payload: map[string]any{}}}}, nil
}

func gatewayInvokeDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerOrchestration, Kind: contracts.KindService,
		Name: "studio.gateway.invoke", Version: "1.0.0", Title: "Invoke a capability on a managed project",
		Description: "Invoke one capability version on a managed project through its Capability Gateway. The idempotency key makes a retried call resolve to the same invocation; the record is stored per project.",
		Domain:      "studio", Entity: "studio_run", Permission: InvokePermission,
		Risk: contracts.RiskHigh, Approval: contracts.ApprovalAuto,
		SuccessEvents: []string{"studio.gateway.invoked"},
		Idempotency:   contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:      "go:studio.gateway.invoke", Tags: []string{"studio", "gateway"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "capability": text(1, 160), "version": text(1, 32),
			"input": {Type: "object", Properties: map[string]*contracts.Schema{}},
			"key":   text(1, 180),
			"mode":  {Type: "string", Enum: []string{"direct"}},
		}, "project", "capability", "version", "key"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"result": contracts.FreeObject(),
		}, "result"),
		Loggable: []string{"project", "capability", "version", "mode"},
	}
}

func (m *Module) gatewayInvoke(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, project, err := m.ownedProject(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(InvokePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	capability := stringField(in, "capability")
	version := stringField(in, "version")
	key := stringField(in, "key")
	mode := stringField(in, "mode")
	if mode == "" {
		mode = "direct"
	}
	if mode != "direct" {
		return contracts.Output{}, contracts.Problem("未知的执行方式")
	}
	if capability == "" || version == "" || key == "" {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	input, _ := in["input"].(map[string]any)
	session, err := remoteGateway.session(sessionKey(p, project.ID))
	if err != nil {
		return contracts.Output{}, err
	}
	origin, err := runningBackendOrigin(ctx, ref(project))
	if err != nil {
		return contracts.Output{}, err
	}
	if origin != session.origin {
		return contracts.Output{}, contracts.Problem("unavailable")
	}
	result, err := remoteGateway.invoke(origin, session, capability, version, input, key)
	if err != nil {
		return contracts.Output{}, err
	}
	record := map[string]any{
		"mode": mode, "capability": capability, "version": version, "key": key,
		"status": result["status"], "invocationId": result["invocationId"],
		"approvalId": result["approvalId"], "runId": runID(result),
	}
	if _, err := dbgen.New(m.pool).StudioRunUpsert(ctx, dbgen.StudioRunUpsertParams{
		TenantID: p.TenantID, ID: project.ID, Mode: mode, Capability: capability, Version: version,
		RequestKey: key, Status: textOr(result["status"]), InvocationID: textOr(result["invocationId"]),
		ApprovalID: intOr(result["approvalId"]), RunID: intOr(result["runId"]),
	}); err != nil {
		return contracts.Output{}, err
	}
	_ = record
	return contracts.Output{Data: map[string]any{"result": result}, Summary: "gateway.invoke=" + capability, Events: []contracts.Event{{Type: "studio.gateway.invoked", AggregateType: "studio_run", AggregateID: invocationID(result), Payload: map[string]any{}}}}, nil
}

func runID(result map[string]any) int64 {
	if data, ok := result["data"].(map[string]any); ok {
		return intOr(data["runId"])
	}
	return 0
}

func textOr(value any) string {
	text, _ := value.(string)
	return text
}

func intOr(value any) int64 {
	if number, ok := value.(float64); ok {
		return int64(number)
	}
	return 0
}

func runListDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.run.list", Version: "1.0.0", Title: "List gateway invocation records",
		Description: "List the recorded gateway invocations of one managed project, newest first.",
		Domain:      "studio", Entity: "studio_run", Permission: ReadPermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true,
		Executor: "go:studio.run.list", Tags: []string{"studio", "read"},
		Input: contracts.Object(map[string]*contracts.Schema{"project": contracts.Int(1, 1<<53)}, "project"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"runs": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": contracts.Int(1, 1<<53), "capability": {Type: "string"}, "status": {Type: "string"},
			})},
		}, "runs"),
	}
}

func (m *Module) runList(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, project, err := m.ownedProject(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(ReadPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	rows, err := dbgen.New(m.pool).StudioRunList(ctx, dbgen.StudioRunListParams{
		TenantID: p.TenantID, ProjectID: project.ID, CreatedBy: p.Account.ID,
	})
	if err != nil {
		return contracts.Output{}, err
	}
	runs := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, map[string]any{
			"id": row.ID, "projectId": row.ProjectID, "mode": row.Mode, "capability": row.Capability,
			"version": row.Version, "key": row.RequestKey, "status": row.Status,
			"invocationId": row.InvocationID, "approvalId": row.ApprovalID, "runId": row.RunID,
		})
	}
	return contracts.Output{Data: map[string]any{"runs": runs}, Summary: "runs=" + itoa(len(runs))}, nil
}

func summaryDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.summary", Version: "1.0.0", Title: "Project workbench summary",
		Description: "Return the collected requirements, work items and gateway invocation records of one managed project in one read.",
		Domain:      "studio", Entity: "studio_project", Permission: ReadPermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true,
		Executor: "go:studio.summary", Tags: []string{"studio", "read"},
		Input: contracts.Object(map[string]*contracts.Schema{"project": contracts.Int(1, 1<<53)}, "project"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"requirements": {Type: "array"}, "items": {Type: "array"}, "runs": {Type: "array"},
		}, "requirements", "items", "runs"),
	}
}

// summary composes the three read lists through the gateway so the DSH client
// needs a single call per project switch.
func (m *Module) summary(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	requirements, err := m.requirementList(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	items, err := m.itemList(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	runs, err := m.runList(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	requirementData, _ := requirements.Data.(map[string]any)
	itemData, _ := items.Data.(map[string]any)
	runData, _ := runs.Data.(map[string]any)
	return contracts.Output{Data: map[string]any{
		"requirements": requirementData["requirements"],
		"items":        itemData["items"],
		"runs":         runData["runs"],
	}}, nil
}

// invocationID extracts the numeric invocation id for the audit event payload.
func invocationID(result map[string]any) string {
	id, _ := result["invocationId"].(string)
	return id
}
