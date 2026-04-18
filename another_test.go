package main

import "testing"

func TestAnother(t *testing.T) {
    if 1+1 != 2 {
        t.Error("basic math failed")
    }
}
