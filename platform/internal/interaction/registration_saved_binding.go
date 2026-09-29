package interaction

import (
	"errors"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

// BindSavedRegistration freezes the grounded command and its original update key before persistence.
func BindSavedRegistration(evidence string, id int64, plan agent.Plan, input agent.Input, saved *SavedPlan) error {
	var err error
	if plan.RegistrationAction != nil && plan.RegistrationAction.Name == agent.RegistrationAdminAssign {
		saved.RegistrationAssignment, saved.RegistrationMenu, err = BindAdminAssignment(evidence, plan, input)
	} else {
		saved.RegistrationCommand, saved.RegistrationMenu, err = BindRegistrationPlan(evidence, plan, input)
	}
	if err != nil {
		return err
	}
	return bindSavedRegistrationKeys(id, saved)
}
func bindSavedRegistrationKeys(id int64, saved *SavedPlan) error {
	if saved.RegistrationCommand != nil && saved.RegistrationAssignment != nil {
		return errors.New("ambiguous saved registration command")
	}
	if saved.RegistrationCommand == nil && saved.RegistrationAssignment == nil {
		return nil
	}
	if id <= 0 {
		return errors.New("missing registration update identity")
	}
	if saved.RegistrationCommand != nil && saved.RegistrationCommand.Key == "" {
		saved.RegistrationCommand.Key = "tg-registration-" + strconv.FormatInt(id, 10)
	}
	if saved.RegistrationAssignment != nil && saved.RegistrationAssignment.Key == "" {
		saved.RegistrationAssignment.Key = "tg-admin-assignment-" + strconv.FormatInt(id, 10)
	}
	return nil
}
