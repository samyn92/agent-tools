package main

import "testing"

func TestPlatformV2(t *testing.T) {
    result := 10 - 5
    if result != 5 {
        t.Errorf("Expected 5, got %d", result)
    }
}
