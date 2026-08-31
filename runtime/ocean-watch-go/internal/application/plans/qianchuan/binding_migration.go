package qianchuan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
)

type BindingAuditCommand struct {
	AdvertiserID     string
	AuthAccountID    string
	TemplateID       string
	CreatorID        string
	CreatorVisibleID string
	ProductIDs       []string
	PlanType         string
	Business         string
	BusinessDate     string
}

type BindingAuditResult struct {
	Mode         string                            `json:"mode"`
	Channel      string                            `json:"channel"`
	GroupID      string                            `json:"group_id"`
	BusinessDate string                            `json:"business_date"`
	Identity     domainqianchuan.PlanGroupIdentity `json:"identity"`
	Status       string                            `json:"status"`
	Candidates   []ExistingPlan                    `json:"candidates"`
	Binding      *PlanBinding                      `json:"binding,omitempty"`
	Inventory    BindingAuditInventory             `json:"inventory"`
}

type BindingAuditInventory struct {
	StartTime  string   `json:"start_time"`
	EndTime    string   `json:"end_time"`
	PageCount  int      `json:"page_count"`
	RequestIDs []string `json:"request_ids,omitempty"`
}

type BindPlanCommand struct {
	BindingAuditCommand
	GroupID string
	AdID    string
	Submit  bool
}

type BindPlanResult struct {
	Mode         string             `json:"mode"`
	Channel      string             `json:"channel"`
	Status       string             `json:"status"`
	GroupID      string             `json:"group_id"`
	BusinessDate string             `json:"business_date"`
	AdID         string             `json:"ad_id"`
	Binding      *PlanBinding       `json:"binding,omitempty"`
	Audit        BindingAuditResult `json:"audit"`
}

type BindingMigrationService struct {
	Tokens     authapplication.TokenProvider
	Reconciler CurrentPlanFinder
	Bindings   PlanBindingStore
	Locks      sharedplans.AdvertiserLocker
	Now        func() time.Time
}

func (service BindingMigrationService) Audit(
	ctx context.Context,
	command BindingAuditCommand,
) (BindingAuditResult, error) {
	identity, groupID, normalized, err := normalizeBindingAuditCommand(command)
	if err != nil {
		return BindingAuditResult{}, err
	}
	if service.Tokens == nil || service.Reconciler == nil || service.Bindings == nil {
		return BindingAuditResult{}, errors.New("Qianchuan binding audit dependencies are incomplete")
	}
	lease, err := service.Tokens.Ensure(ctx, authapplication.TokenQuery{
		Channel: "qianchuan", AdvertiserID: identity.AdvertiserID,
		AuthAccountID: normalized.AuthAccountID,
	})
	if err != nil {
		return BindingAuditResult{}, err
	}
	scoped, err := authapplication.WithAdvertiserTokenLease(ctx, lease, identity.AdvertiserID)
	if err != nil {
		return BindingAuditResult{}, err
	}
	return service.auditWithToken(scoped, normalized, identity, groupID, lease.AccessToken)
}

func (service BindingMigrationService) Bind(
	ctx context.Context,
	command BindPlanCommand,
) (BindPlanResult, error) {
	identity, groupID, normalized, err := normalizeBindingAuditCommand(command.BindingAuditCommand)
	if err != nil {
		return BindPlanResult{}, err
	}
	command.GroupID = strings.TrimSpace(command.GroupID)
	command.AdID = strings.TrimSpace(command.AdID)
	if command.GroupID != groupID {
		return BindPlanResult{}, errors.New("confirmed group_id does not match the complete plan identity")
	}
	if !validPositiveID(command.AdID) {
		return BindPlanResult{}, errors.New("ad_id must be a positive decimal ID")
	}
	if service.Tokens == nil || service.Reconciler == nil || service.Bindings == nil {
		return BindPlanResult{}, errors.New("Qianchuan plan binding dependencies are incomplete")
	}
	lease, err := service.Tokens.Ensure(ctx, authapplication.TokenQuery{
		Channel: "qianchuan", AdvertiserID: identity.AdvertiserID,
		AuthAccountID: normalized.AuthAccountID,
	})
	if err != nil {
		return BindPlanResult{}, err
	}
	scoped, err := authapplication.WithAdvertiserTokenLease(ctx, lease, identity.AdvertiserID)
	if err != nil {
		return BindPlanResult{}, err
	}
	var result BindPlanResult
	bind := func(locked context.Context) error {
		audit, auditErr := service.auditWithToken(locked, normalized, identity, groupID, lease.AccessToken)
		if auditErr != nil {
			return auditErr
		}
		candidateFound := false
		for _, candidate := range audit.Candidates {
			if candidate.AdID == command.AdID {
				candidateFound = true
				break
			}
		}
		if audit.Binding != nil && audit.Binding.AdID == command.AdID && audit.Status == "bound" {
			candidateFound = true
		}
		if !candidateFound {
			return errors.New("confirmed ad_id is not an exact candidate for this business date and group")
		}
		result = BindPlanResult{
			Mode: "dry_run", Channel: "qianchuan", Status: "would_bind",
			GroupID: groupID, BusinessDate: normalized.BusinessDate, AdID: command.AdID, Audit: audit,
		}
		if !command.Submit {
			return nil
		}
		binding, bindingErr := NewPlanBinding(identity, groupID, normalized.BusinessDate, command.AdID, service.now())
		if bindingErr != nil {
			return bindingErr
		}
		if bindingErr = service.Bindings.Put(locked, binding); bindingErr != nil {
			return bindingErr
		}
		result.Mode, result.Status, result.Binding = "submit", "bound", &binding
		return nil
	}
	if command.Submit {
		if service.Locks == nil {
			return BindPlanResult{}, errors.New("Qianchuan plan binding advertiser lock is required")
		}
		err = sharedplans.WithAdvertiserLock(scoped, service.Locks, domainplans.WriteScope{
			Channel: domainplans.ChannelQianchuan, AdvertiserID: identity.AdvertiserID,
			LockFamily: domainplans.LockQianchuanWorks,
		}, bind)
	} else {
		err = bind(scoped)
	}
	return result, err
}

func (service BindingMigrationService) auditWithToken(
	ctx context.Context,
	command BindingAuditCommand,
	identity domainqianchuan.PlanGroupIdentity,
	groupID string,
	accessToken string,
) (BindingAuditResult, error) {
	discovery, err := service.Reconciler.FindCurrentPlans(ctx, CurrentPlanRequest{
		AdvertiserID: identity.AdvertiserID, AccessToken: accessToken,
		Targets: []CreatorTarget{{
			AwemeID: identity.CreatorID, VisibleID: command.CreatorVisibleID,
			ProductIDs: identity.ProductIDs, GroupID: groupID,
			BusinessDate: command.BusinessDate, Identity: identity,
		}},
	})
	if err != nil {
		return BindingAuditResult{}, fmt.Errorf("audit Qianchuan plan binding candidates: %w", err)
	}
	policy, exists := discovery.Policies[groupID]
	if !exists {
		return BindingAuditResult{}, errors.New("Qianchuan binding audit omitted the group policy")
	}
	result := BindingAuditResult{
		Mode: "audit", Channel: "qianchuan", GroupID: groupID, BusinessDate: command.BusinessDate,
		Identity: identity, Status: policy.Status, Candidates: append([]ExistingPlan(nil), policy.Candidates...),
		Inventory: BindingAuditInventory{
			StartTime: discovery.StartTime, EndTime: discovery.EndTime, PageCount: discovery.PageCount,
			RequestIDs: append([]string(nil), discovery.RequestIDs...),
		},
	}
	if binding, found, bindingErr := service.Bindings.Get(ctx, command.BusinessDate, groupID); bindingErr != nil {
		return BindingAuditResult{}, bindingErr
	} else if found {
		result.Binding = &binding
	}
	return result, nil
}

func normalizeBindingAuditCommand(command BindingAuditCommand) (
	domainqianchuan.PlanGroupIdentity,
	string,
	BindingAuditCommand,
	error,
) {
	command.AdvertiserID = strings.TrimSpace(command.AdvertiserID)
	command.AuthAccountID = strings.TrimSpace(command.AuthAccountID)
	command.TemplateID = strings.TrimSpace(command.TemplateID)
	command.CreatorID = strings.TrimSpace(command.CreatorID)
	command.CreatorVisibleID = strings.TrimSpace(command.CreatorVisibleID)
	command.PlanType = strings.TrimSpace(command.PlanType)
	command.Business = strings.TrimSpace(command.Business)
	command.BusinessDate = strings.TrimSpace(command.BusinessDate)
	if err := ValidateBusinessDate(command.BusinessDate); err != nil {
		return domainqianchuan.PlanGroupIdentity{}, "", BindingAuditCommand{}, err
	}
	identity, err := domainqianchuan.NewPlanGroupIdentity(
		command.AdvertiserID, command.TemplateID, command.CreatorID,
		command.ProductIDs, command.PlanType, command.Business,
	)
	if err != nil {
		return domainqianchuan.PlanGroupIdentity{}, "", BindingAuditCommand{}, err
	}
	groupID, err := domainqianchuan.GroupID(identity)
	if err != nil {
		return domainqianchuan.PlanGroupIdentity{}, "", BindingAuditCommand{}, err
	}
	command.ProductIDs = append([]string(nil), identity.ProductIDs...)
	return identity, groupID, command, nil
}

func (service BindingMigrationService) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}
