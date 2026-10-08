package help

import "testing"

func TestRun_UnknownTopicErrors(t *testing.T) {
	if err := Run([]string{"help", "nope"}); err == nil {
		t.Fatal("Run([\"help\", \"nope\"]) = nil error, want non-nil")
	}
}

func TestRun_KnownTopicSucceeds(t *testing.T) {
	if err := Run([]string{"help", "module"}); err != nil {
		t.Fatalf("Run([\"help\", \"module\"]) = %v, want nil", err)
	}
}

func TestRun_EnvironmentTopicSucceeds(t *testing.T) {
	if err := Run([]string{"help", "environment"}); err != nil {
		t.Fatalf("Run([\"help\", \"environment\"]) = %v, want nil", err)
	}
}

func TestRun_SettingsTopicSucceeds(t *testing.T) {
	if err := Run([]string{"help", "settings"}); err != nil {
		t.Fatalf("Run([\"help\", \"settings\"]) = %v, want nil", err)
	}
}

func TestRun_SetupTopicSucceeds(t *testing.T) {
	if err := Run([]string{"help", "setup"}); err != nil {
		t.Fatal(err)
	}
}
