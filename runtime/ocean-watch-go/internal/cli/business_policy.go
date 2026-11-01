package cli

import (
	"strconv"
	"strings"

	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
)

func (runner Runner) guardTokens(provider auth.TokenProvider, required ...string) auth.TokenProvider {
	instance := policy.Default()
	if runner.Policy != nil {
		instance = *runner.Policy
	}
	var authorizations auth.AuthorizationStore
	if manager, ok := provider.(*auth.TokenManager); ok {
		authorizations = manager.Authorizations
	} else if store, ok := runner.Authorizations.(auth.AuthorizationStore); ok {
		authorizations = store
	}
	return policy.Tokens{Instance: instance, Provider: provider, Required: required, Authorizations: authorizations}
}

func submitted(args []string) bool {
	for _, arg := range args {
		if arg == "--submit" {
			return true
		}
		if strings.HasPrefix(arg, "--submit=") {
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, "--submit="))
			if err != nil || value {
				return true
			}
		}
	}
	return false
}

func checkCLIInstance(instance policy.Instance, spec contracts.CapabilitySpec, args []string) error {
	minimum := spec.MinimumMode
	if spec.Effect == contracts.EffectOnlineWrite && spec.RequiresSubmit && !submitted(args) {
		minimum = contracts.QueryModePlan
	}
	if err := instance.Check(spec.RequiredToolsets, minimum); err != nil {
		return err
	}
	channel := spec.Channel
	if channel == "shared" {
		channel = argumentValue(args, "--channel")
	}
	if channel != "marketing" && channel != "qianchuan" {
		return nil
	}
	for index, arg := range args {
		name, value, inline := strings.Cut(arg, "=")
		if name != "--advertiser-id" && name != "--advertiser-ids" && name != "--accounts" {
			continue
		}
		if !inline && index+1 < len(args) {
			value = args[index+1]
		}
		for _, id := range strings.Split(value, ",") {
			if id != "" && instance.CheckSubject(channel, "advertiser", strings.TrimSpace(id)) != nil {
				return policy.ErrDenied
			}
		}
	}
	return nil
}
