package main

import (
	"reflect"
	"testing"
)

func TestParseActions(t *testing.T) {
	actions, err := parseActions("analytics:read, order:assign,order:read")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"analytics:read", "order:assign", "order:read"}
	if !reflect.DeepEqual(actions, want) {
		t.Fatalf("parseActions() = %#v, want %#v", actions, want)
	}
}

func TestParseActionsRejectsUnsafeInput(t *testing.T) {
	for _, raw := range []string{"", "order:read,", "order:read,order:read", "root", "ledger:post"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseActions(raw); err == nil {
				t.Fatalf("parseActions(%q) unexpectedly succeeded", raw)
			}
		})
	}
}
