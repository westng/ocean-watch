package contracts

// Command is a stable two-token CLI identity.
// The route is deliberately not configurable by a user command line flag.
type Command struct {
	Domain      string
	Action      string
	Description string
}

func (c Command) Name() string { return c.Domain + " " + c.Action }

// Commands is derived from the capability registry so CLI parsing, help and
// request-budget code cannot acquire a second metadata source.
var Commands = DefaultCapabilityRegistry.Commands()

func Lookup(domain, action string) (Command, bool) {
	return DefaultCapabilityRegistry.Command(domain + " " + action)
}

func HasDomain(domain string) bool {
	for _, command := range Commands {
		if command.Domain == domain {
			return true
		}
	}
	return false
}

func Names() []string {
	result := make([]string, 0, len(Commands))
	for _, command := range Commands {
		result = append(result, command.Name())
	}
	return result
}

// CapabilityFor preserves the existing CLI query API while delegating all
// classifications to the registry.
func CapabilityFor(command Command) Capability {
	spec, ok := DefaultCapabilityRegistry.ByCLICommand(command.Name())
	if !ok {
		return Capability{}
	}
	return Capability{
		Channel: spec.Channel, Effect: spec.Effect, RequiresSubmit: spec.RequiresSubmit,
	}
}
