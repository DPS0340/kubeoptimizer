package cmd

import "testing"

func TestValidateOutput(t *testing.T) {
	if err := validateOutput("table"); err != nil {
		t.Fatal(err)
	}
	if err := validateOutput("json"); err != nil {
		t.Fatal(err)
	}
	if err := validateOutput("yaml"); err == nil {
		t.Fatal("yaml must be rejected")
	}
}
