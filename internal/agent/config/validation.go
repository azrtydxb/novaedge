package config

import pkgerrors "github.com/piwi3910/novaedge/internal/pkg/errors"

// Validator provides configuration validation
type Validator struct{}

// NewValidator creates a new configuration validator
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateSnapshot validates a complete configuration snapshot
func (v *Validator) ValidateSnapshot(snapshot *Snapshot) error {
	if snapshot == nil || snapshot.ConfigSnapshot == nil {
		return pkgerrors.NewValidationError("snapshot cannot be nil")
	}
	
	if snapshot.Version == "" {
		return pkgerrors.NewValidationError("version is required").WithField("field", "version")
	}
	
	return nil
}
