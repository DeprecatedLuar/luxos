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
