package main

import "testing"

func TestGetenvReturnsDefaultWhenUnset(t *testing.T) {
	t.Setenv("__SCRIPTORUM_TEST_ENV__", "")
	if got := getenv("__SCRIPTORUM_TEST_ENV__", "fallback"); got != "fallback" {
		t.Fatalf("want fallback got %q", got)
	}
}

func TestGetenvReturnsValueWhenSet(t *testing.T) {
	t.Setenv("__SCRIPTORUM_TEST_ENV__", "value")
	if got := getenv("__SCRIPTORUM_TEST_ENV__", "fallback"); got != "value" {
		t.Fatalf("want value got %q", got)
	}
}
