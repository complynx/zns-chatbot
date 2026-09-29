package bot

import (
	"errors"
	"strconv"
	"strings"
	"unicode"

	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

var errPassBatchSyntax = errors.New("invalid pass batch command")

// Split command arguments without shell expansion; quotes preserve names/comments.
func passBatchWords(text string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote rune
	active := false
	for _, r := range text {
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			active = true
			continue
		}
		if unicode.IsSpace(r) {
			if active {
				words = append(words, word.String())
				word.Reset()
				active = false
			}
			continue
		}
		word.WriteRune(r)
		active = true
	}
	if quote != 0 {
		return nil, errPassBatchSyntax
	}
	if active {
		words = append(words, word.String())
	}
	return words, nil
}

const passBatchAssign = "admin_assign"
const passBatchCancel = "admin_cancel"
const passBatchUncouple = "admin_uncouple"
const passBatchTier = "tier"
const passBatchEventOption = "--pass_key"

func parsePassBatch(text string) (passbooking.RuntimeBatch, error) {
	words, err := passBatchWords(text)
	if err != nil || len(words) == 0 {
		return passbooking.RuntimeBatch{}, errPassBatchSyntax
	}
	result, arguments, err := passBatchAction(words)
	if err != nil {
		return result, err
	}
	options := map[string]string{}
	for i := 0; i < len(arguments); i++ {
		token := arguments[i]
		if !strings.HasPrefix(token, "--") {
			id, parseErr := strconv.ParseInt(token, 10, 64)
			if parseErr != nil || id <= 0 {
				return result, errPassBatchSyntax
			}
			result.Recipients = append(result.Recipients, id)
			continue
		}
		if _, exists := options[token]; exists {
			return result, errPassBatchSyntax
		}
		value := ""
		switch token {
		case "--skip", "--create_last", "--leader", "--follower":
		default:
			i++
			if i == len(arguments) {
				return result, errPassBatchSyntax
			}
			value = arguments[i]
		}
		options[token] = value
	}
	if err = applyPassBatchOptions(&result, options); err != nil {
		return result, err
	}
	return result, validatePassBatchRecipients(result)
}

func validatePassBatchRecipients(result passbooking.RuntimeBatch) error {
	if result.Action == passBatchTier {
		if len(result.Recipients) > 0 {
			return errPassBatchSyntax
		}
		return nil
	}
	if result.Action == passBatchAssign && result.Event == "" {
		return errPassBatchSyntax
	}
	if len(result.Recipients) == 0 || len(result.Recipients) > passbooking.MaxAdminBatchRecipients {
		return errPassBatchSyntax
	}
	return nil
}
func passBatchAction(words []string) (passbooking.RuntimeBatch, []string, error) {
	var result passbooking.RuntimeBatch
	arguments := words[1:]
	switch words[0] {
	case "/passes_assign":
		result.Action = passBatchAssign
	case "/passes_cancel":
		result.Action = passBatchCancel
	case "/passes_tier":
		result.Action = passBatchTier
	case "/passes_uncouple":
		const uncoupleArguments = 3
		if len(words) != uncoupleArguments {
			return result, nil, errPassBatchSyntax
		}
		result.Action, result.Event, arguments = passBatchUncouple, words[1], words[2:]
	default:
		return result, nil, errPassBatchSyntax
	}
	return result, arguments, nil
}

func applyPassBatchOptions(result *passbooking.RuntimeBatch, options map[string]string) error {
	for option, value := range options {
		if option != passBatchEventOption && result.Action != passBatchAssign {
			return errPassBatchSyntax
		}
		if err := applyPassBatchOption(result, option, value); err != nil {
			return err
		}
	}
	_, leader := options["--leader"]
	_, follower := options["--follower"]
	_, profile := options["--create_last"]
	_, name := options["--create_name"]
	if leader && follower || profile && (leader || follower || name) {
		return errPassBatchSyntax
	}
	if profile {
		result.Options.Create = &passbooking.AdminCreate{FromProfile: true}
	}
	return nil
}

func applyPassBatchOption(result *passbooking.RuntimeBatch, option, value string) error {
	switch option {
	case passBatchEventOption:
		result.Event = value
	case "--skip":
		enabled := true
		result.Options.SkipBalance = &enabled
	case "--create_last":
	case "--leader", "--follower", "--create_name":
		if result.Options.Create == nil {
			result.Options.Create = &passbooking.AdminCreate{}
		}
		if option == "--create_name" {
			result.Options.Create.LegalName = &value
		} else {
			result.Options.Create.Role = passallocation.Role(strings.TrimPrefix(option, "--"))
		}
	case "--price", "--append_to_tier":
		number, err := strconv.Atoi(value)
		if err != nil {
			return errPassBatchSyntax
		}
		if option == "--price" {
			result.Options.TotalPrice = &number
		} else {
			result.Options.AppendTier = &number
		}
	case "--type":
		result.Options.Kind = &value
	case "--comment":
		result.Options.Comment = &value
	default:
		return errPassBatchSyntax
	}
	return nil
}
