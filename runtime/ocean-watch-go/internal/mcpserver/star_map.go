package mcpserver

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

const starMapAuthorizationInputSchema = `{
  "type":"object","additionalProperties":false,
  "properties":{"subject_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"}}
}`

const starMapAuthorizationSuccessSchema = `{
  "type":"object","additionalProperties":false,
  "required":["ok","request_id","source","channel","status","mappings","authorizations"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},
    "source":{"const":"local_state"},"channel":{"const":"star_map"},
    "status":{"type":"object","additionalProperties":false,
      "required":["has_app_id","has_secret","authorization_count","authorized_account_count","authorized_subject_count","generation","subject_id","subject_id_authorized"],
      "properties":{
        "has_app_id":{"type":"boolean"},"has_secret":{"type":"boolean"},
        "authorization_count":{"type":"integer","minimum":0},"authorized_account_count":{"type":"integer","minimum":0},
        "authorized_subject_count":{"type":"integer","minimum":0},"generation":{"type":"integer","minimum":0},
        "subject_id":{"type":["string","null"],"pattern":"^[1-9][0-9]{0,18}$"},"subject_id_authorized":{"type":["boolean","null"]}
      }},
    "mappings":{"type":"array","items":{"type":"object","additionalProperties":false,
      "required":["subject_id","subject_kind","authorization_ids","ambiguous"],"properties":{
        "subject_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"subject_kind":{"const":"star_account"},
        "authorization_ids":{"type":"array","items":{"type":"string"}},"ambiguous":{"type":"boolean"}
      }}},
    "authorizations":{"type":"array","items":{"type":"object","additionalProperties":false,
      "required":["authorization_id","token_revision","has_access_token","has_refresh_token","access_token_expires_at","refresh_token_expires_at","pending_account_sync","subject_ids"],
      "properties":{
        "authorization_id":{"type":"string"},"token_revision":{"type":"integer","minimum":0},
        "has_access_token":{"type":"boolean"},"has_refresh_token":{"type":"boolean"},
        "access_token_expires_at":{"type":"string"},"refresh_token_expires_at":{"type":"string"},
        "pending_account_sync":{"type":"boolean"},"subject_ids":{"type":"array","items":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"}}
      }}}
  }
}`

const starMapAuthorizationOutputSchema = objectOneOf + starMapAuthorizationSuccessSchema + `,` + errorOutputSchema + `]}`

type starMapAuthorizationInput struct {
	SubjectID string `json:"subject_id"`
}

type starMapAuthorizationOutput struct {
	OK             bool                       `json:"ok"`
	RequestID      string                     `json:"request_id"`
	Source         string                     `json:"source"`
	Channel        string                     `json:"channel"`
	Status         starMapAuthorizationStatus `json:"status"`
	Mappings       []starMapSubjectMapping    `json:"mappings"`
	Authorizations []starMapAuthorizationItem `json:"authorizations"`
}

type starMapAuthorizationStatus struct {
	HasAppID               bool    `json:"has_app_id"`
	HasSecret              bool    `json:"has_secret"`
	AuthorizationCount     int     `json:"authorization_count"`
	AuthorizedAccountCount int     `json:"authorized_account_count"`
	AuthorizedSubjectCount int     `json:"authorized_subject_count"`
	Generation             int     `json:"generation"`
	SubjectID              *string `json:"subject_id"`
	SubjectIDAuthorized    *bool   `json:"subject_id_authorized"`
}

type starMapSubjectMapping struct {
	SubjectID        string   `json:"subject_id"`
	SubjectKind      string   `json:"subject_kind"`
	AuthorizationIDs []string `json:"authorization_ids"`
	Ambiguous        bool     `json:"ambiguous"`
}

type starMapAuthorizationItem struct {
	AuthorizationID       string   `json:"authorization_id"`
	TokenRevision         int      `json:"token_revision"`
	HasAccessToken        bool     `json:"has_access_token"`
	HasRefreshToken       bool     `json:"has_refresh_token"`
	AccessTokenExpiresAt  string   `json:"access_token_expires_at"`
	RefreshTokenExpiresAt string   `json:"refresh_token_expires_at"`
	PendingAccountSync    bool     `json:"pending_account_sync"`
	SubjectIDs            []string `json:"subject_ids"`
}

func (runtime Runtime) addStarMapTools(server *mcp.Server) {
	no := false
	addCapabilityTool(server, "star_map.authorization_inspect", &mcp.Tool{
		Name:        "get_star_map_authorization",
		Description: "查看本机星图应用、Token 存在状态与星图账号到授权的映射。可按 subject_id 精确筛选，只读取本地状态，不刷新 Token、不调用官方接口，也不返回凭据值。星图账号使用 subject_kind=star_account，不是营销或千川广告主。",
		Annotations: &mcp.ToolAnnotations{Title: "查看星图授权状态", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no},
		InputSchema: json.RawMessage(starMapAuthorizationInputSchema), OutputSchema: json.RawMessage(starMapAuthorizationOutputSchema),
	}, runtime.handler(runtime.getStarMapAuthorization))
}

func (runtime Runtime) getStarMapAuthorization(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	const tool = "get_star_map_authorization"
	started, requestID := runtime.now(), runtime.requestID()
	var input starMapAuthorizationInput
	if err := decodeStrict(request.Params.Arguments, &input); err != nil || !optionalDecimalID(input.SubjectID) {
		return runtime.failureResult(started, requestID, tool, invalidArgumentFailure()), nil
	}
	if runtime.StarMapAuth == nil {
		return runtime.failureResult(started, requestID, tool, internalFailure()), nil
	}
	result, err := runtime.StarMapAuth.Inspect(ctx, authapplication.StatusQuery{Channel: "star_map", AdvertiserID: input.SubjectID})
	if err != nil {
		failure := mapLocalQueryError(err)
		var domainErr *domain.Error
		if errors.As(err, &domainErr) && domainErr.Code == "authorization_not_found" {
			failure = toolFailure{Code: "AUTHORIZATION_NOT_FOUND", Message: "Star Map authorization was not found", Details: map[string]any{}}
		}
		return runtime.failureResult(started, requestID, tool, failure), nil
	}
	output := starMapAuthorizationOutput{
		OK: true, RequestID: requestID, Source: "local_state", Channel: "star_map",
		Status: starMapAuthorizationStatus{
			HasAppID: result.Status.HasAppID, HasSecret: result.Status.HasSecret,
			AuthorizationCount: result.Status.AuthorizationCount, AuthorizedAccountCount: result.Status.AuthorizedAccountCount,
			AuthorizedSubjectCount: result.Status.AuthorizedAdvertiserCount, Generation: result.Status.Generation,
			SubjectIDAuthorized: result.Status.AdvertiserIDAuthorized,
		},
		Mappings:       make([]starMapSubjectMapping, 0, len(result.Mappings.Mappings)),
		Authorizations: make([]starMapAuthorizationItem, 0, len(result.Mappings.Authorizations)),
	}
	if input.SubjectID != "" {
		output.Status.SubjectID = &input.SubjectID
	}
	statusByID := map[string]authapplication.AuthorizationStatus{}
	for _, row := range result.Status.Authorizations {
		statusByID[row.AuthorizationID] = row
	}
	for _, row := range result.Mappings.Mappings {
		output.Mappings = append(output.Mappings, starMapSubjectMapping{
			SubjectID: row.AdvertiserID, SubjectKind: "star_account", AuthorizationIDs: append([]string{}, row.AuthorizationIDs...), Ambiguous: row.Ambiguous,
		})
	}
	for _, row := range result.Mappings.Authorizations {
		status := statusByID[row.AuthorizationID]
		output.Authorizations = append(output.Authorizations, starMapAuthorizationItem{
			AuthorizationID: row.AuthorizationID, TokenRevision: row.TokenRevision,
			HasAccessToken: row.HasAccessToken, HasRefreshToken: row.HasRefreshToken,
			AccessTokenExpiresAt: status.AccessTokenExpiresAt, RefreshTokenExpiresAt: status.RefreshTokenExpiresAt,
			PendingAccountSync: row.PendingAccountSync, SubjectIDs: append([]string{}, row.AdvertiserIDs...),
		})
	}
	return runtime.successResult(started, requestID, tool, output), nil
}
