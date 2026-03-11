package controller

import (
	"testing"
)

func TestConditionReasonConstants(t *testing.T) {
	if ConditionReasonValid != "Valid" {
		t.Errorf("ConditionReasonValid = %q, want %q", ConditionReasonValid, "Valid")
	}
	if ConditionReasonValidationFailed != "ValidationFailed" {
		t.Errorf("ConditionReasonValidationFailed = %q, want %q", ConditionReasonValidationFailed, "ValidationFailed")
	}
}

func TestKindGatewayConstant(t *testing.T) {
	if kindGateway != "Gateway" {
		t.Errorf("kindGateway = %q, want %q", kindGateway, "Gateway")
	}
}

func TestTriggerConfigUpdate_NilServer(_ *testing.T) {
	triggerConfigUpdate(nil)
}
