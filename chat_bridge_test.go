package main

import "testing"

func TestShouldMirrorChat(t *testing.T) {
	if !shouldMirrorChat("alice", nil) {
		t.Fatal("empty hosts = all")
	}
	if !shouldMirrorChat("alice", []string{"alice", "bob"}) {
		t.Fatal("allowlisted")
	}
	if shouldMirrorChat("eve", []string{"alice"}) {
		t.Fatal("not allowlisted")
	}
	if !shouldMirrorChat("Alice", []string{"alice"}) {
		t.Fatal("case fold")
	}
}

func TestEnsureWSQuery(t *testing.T) {
	u := ensureWSQuery("ws://127.0.0.1:9876/", map[string]string{"nick": "bridge", "role": "bridge"})
	if u == "" || u == "ws://127.0.0.1:9876/" {
		t.Fatalf("expected query: %s", u)
	}
}
