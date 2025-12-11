package utils_test

import (
	"os"
	"testing"

	"github.com/voluzi/cosmoseed/internal/utils"
)

func TestEnviron(t *testing.T) {
	if os.Getenv("integer") != "" {
		t.Fatalf("wrong initialization")
	}

	if os.Getenv("unsigned") != "" {
		t.Fatalf("wrong initialization")
	}

	if os.Getenv("string") != "" {
		t.Fatalf("wrong initialization")
	}

	if utils.GetInt("integer", -1) != -1 {
		t.Fatalf("wanted -1")
	}

	if utils.GetUint64("unsigned", 10) != 10 {
		t.Fatalf("wanted 10")
	}

	if utils.GetString("string", "example") != "example" {
		t.Fatalf("wanted example")
	}

	integer, unsigned, str := "-1", "10", "example"

	if err := os.Setenv("integer", integer); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := os.Setenv("unsigned", unsigned); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := os.Setenv("string", str); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if utils.GetInt("integer", -5) != -1 {
		t.Fatalf("wanted -1")
	}

	if utils.GetUint64("unsigned", 15) != 10 {
		t.Fatalf("wanted 10")
	}

	if utils.GetString("string", "invalid") != "example" {
		t.Fatalf("wanted example")
	}
}
