package setup

import (
	"errors"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) handleProviderFormSave() error {
	p := &m.editingProvider
	p.Name = strings.TrimSpace(p.Name)
	p.APIKey = strings.TrimSpace(p.APIKey)
	p.BaseURL = strings.TrimSpace(p.BaseURL)
	p.Model = strings.TrimSpace(p.Model)
	for _, item := range m.providerFormItems() {
		if item.Required != "" && strings.TrimSpace(item.Row.Status) == "" {
			return errors.New(item.Required)
		}
	}
	if err := setup.CheckProvider(*p); err != nil {
		return err
	}
	m.cfg.SetProvider(*p)
	m.cfg.ActiveProviderID = p.ID
	return m.commitFormAndReturn(screenProviderList)
}
