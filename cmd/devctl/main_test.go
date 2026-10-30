package main

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	pos, flags := parse([]string{"start", "Dentio", "feat/x", "--task", "gör saker", "--json", "--base=main", "%12"})
	if !reflect.DeepEqual(pos, []string{"start", "Dentio", "feat/x", "%12"}) {
		t.Errorf("pos = %v", pos)
	}
	want := map[string]string{"task": "gör saker", "json": "", "base": "main"}
	if !reflect.DeepEqual(flags, want) {
		t.Errorf("flags = %v", flags)
	}
}
