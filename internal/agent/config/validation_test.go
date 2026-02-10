package config

import (
	"errors"
	"testing"

	pkgerrors "github.com/piwi3910/novaedge/internal/pkg/errors"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewValidator(t *testing.T) {
	v := NewValidator()
	if v == nil {
		t.Fatal("expected non-nil validator")
	}
}

func TestValidateSnapshot_NilSnapshot(t *testing.T) {
	v := NewValidator()

	err := v.ValidateSnapshot(nil)
	if err == nil {
		t.Fatal("expected error for nil snapshot")
	}

	var validationErr *pkgerrors.ValidationError
	if !errors.As(err, &validationErr) {
		t.Errorf("expected ValidationError, got %T", err)
	}
}

func TestValidateSnapshot_NilConfigSnapshot(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{ConfigSnapshot: nil}
	err := v.ValidateSnapshot(snapshot)
	if err == nil {
		t.Fatal("expected error for nil ConfigSnapshot")
	}

	var validationErr *pkgerrors.ValidationError
	if !errors.As(err, &validationErr) {
		t.Errorf("expected ValidationError, got %T", err)
	}
}

func TestValidateSnapshot_EmptyVersion(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "",
		},
	}
	err := v.ValidateSnapshot(snapshot)
	if err == nil {
		t.Fatal("expected error for empty version")
	}

	var validationErr *pkgerrors.ValidationError
	if !errors.As(err, &validationErr) {
		t.Errorf("expected ValidationError, got %T", err)
	}
}

func TestValidateSnapshot_ValidSnapshot(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
		},
	}
	err := v.ValidateSnapshot(snapshot)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateSnapshot_ValidSnapshotWithData(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version:        "v2",
			GenerationTime: 1234567890,
			Gateways:       []*pb.Gateway{},
			Routes:         []*pb.Route{},
			Clusters:       []*pb.Cluster{},
		},
	}
	err := v.ValidateSnapshot(snapshot)
	if err != nil {
		t.Fatalf("expected no error for valid snapshot with data, got %v", err)
	}
}
