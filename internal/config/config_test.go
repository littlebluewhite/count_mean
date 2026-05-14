package config

import (
	"testing"
)

func TestValidate_DefaultConfig_Accepted(t *testing.T) {
	c := DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Errorf("DefaultConfig 應通過 Validate，got %v", err)
	}
}

func TestValidate_OutputDirTraversal_Rejected(t *testing.T) {
	c := DefaultConfig()
	c.OutputDir = "../../etc"
	if err := c.Validate(); err == nil {
		t.Error("expected err for traversal path, got nil")
	}
}

func TestValidate_OutputDirSystemDir_Rejected(t *testing.T) {
	c := DefaultConfig()
	c.OutputDir = "/etc"
	if err := c.Validate(); err == nil {
		t.Error("expected err for system dir, got nil")
	}
}

func TestValidate_OutputDirRelativePath_Accepted(t *testing.T) {
	c := DefaultConfig()
	c.OutputDir = "./output"
	if err := c.Validate(); err != nil {
		t.Errorf("expected nil err for ./output, got %v", err)
	}
}

func TestValidate_OutputDirNullByte_Rejected(t *testing.T) {
	c := DefaultConfig()
	c.OutputDir = "out\x00put"
	if err := c.Validate(); err == nil {
		t.Error("expected err for null byte path, got nil")
	}
}

func TestValidate_InputDirTraversal_Rejected(t *testing.T) {
	c := DefaultConfig()
	c.InputDir = "../../etc"
	if err := c.Validate(); err == nil {
		t.Error("expected err for InputDir traversal, got nil")
	}
}
