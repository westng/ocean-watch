package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type accountOptions struct {
	configPath    string
	channel       string
	advertiserID  string
	authAccountID string
	name          string
	all           bool
}

func parseAccountOptions(action string, args []string) (accountOptions, error) {
	options := accountOptions{}
	flags := flag.NewFlagSet("accounts "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.channel, "channel", "", "")
	flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.StringVar(&options.name, "name", "", "")
	flags.BoolVar(&options.all, "all", false, "")
	if err := flags.Parse(args); err != nil {
		return accountOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return accountOptions{}, errors.New("unexpected positional account arguments")
	}
	if action != "list" && (options.channel == "" || options.advertiserID == "") {
		return accountOptions{}, errors.New("--channel and --advertiser-id are required for this action")
	}
	if action == "add" && strings.TrimSpace(options.name) == "" {
		return accountOptions{}, errors.New("--name is required when adding an account")
	}
	if action != "add" && options.authAccountID != "" {
		return accountOptions{}, errors.New("--auth-account-id is only valid when adding an account")
	}
	return options, nil
}

func RunAccounts(ctx context.Context, action string, args []string, store application.AccountStore, stdout io.Writer) int {
	options, err := parseAccountOptions(action, args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	var selected *domain.Channel
	if options.channel != "" {
		channel, err := domain.ParseChannel(options.channel)
		if err != nil {
			WriteDomainError(stdout, domain.NewError("invalid_channel", err.Error(), 2, nil))
			return 2
		}
		selected = &channel
	}
	if action == "list" {
		book, err := store.Read(ctx)
		if err != nil {
			WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to read account configuration", 2, err))
			return 2
		}
		accounts := book.List(selected, !options.all)
		_ = WriteJSON(stdout, AccountListEnvelope{
			OK: true, Accounts: accounts,
			Presentation: domain.ManagedAccountPresentation(accounts, options.all),
		})
		return 0
	}
	channel := *selected
	var result domain.ManagedAccount
	operation := ""
	_, err = store.Update(ctx, func(book *domain.AccountBook) error {
		switch action {
		case "add":
			account := domain.ManagedAccount{
				Channel: channel, AdvertiserID: options.advertiserID,
				Name: options.name, Enabled: true, AuthAccountID: options.authAccountID,
			}
			for _, current := range book.Accounts[channel] {
				if current.AdvertiserID == options.advertiserID {
					account.Enabled = current.Enabled
					if options.authAccountID == "" {
						account.AuthAccountID = current.AuthAccountID
					}
				}
			}
			var created bool
			var updateErr error
			result, created, updateErr = book.Upsert(account)
			if created {
				operation = "created"
			} else {
				operation = "updated"
			}
			return updateErr
		case "remove":
			var updateErr error
			result, updateErr = book.Remove(channel, options.advertiserID)
			operation = "removed"
			return updateErr
		case "enable", "disable":
			var updateErr error
			result, updateErr = book.SetEnabled(channel, options.advertiserID, action == "enable")
			operation = action + "d"
			return updateErr
		default:
			return errors.New("unsupported account action")
		}
	})
	if err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", err.Error(), 2, err))
		return 2
	}
	if action == "remove" {
		_ = WriteJSON(stdout, AccountRemovalEnvelope{
			OK: true, Action: operation,
			Account: AccountReference{Channel: result.Channel, AdvertiserID: result.AdvertiserID},
		})
		return 0
	}
	_ = WriteJSON(stdout, AccountMutationEnvelope{OK: true, Action: operation, Account: result})
	return 0
}
