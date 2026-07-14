package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleAPIChatGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/chat", nil)
	rr := httptest.NewRecorder()
	HandleAPIChat(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["persona"] != "glyph-sphere" {
		t.Fatalf("%v", m)
	}
}

func TestHandleAPIChatOfflineReply(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("GROK_API_KEY", "")
	t.Setenv("XAI_KEY", "")
	t.Setenv("GY_CHAT_LOCAL", "1")
	body := bytes.NewBufferString(`{"message":"hello sphere","session":"test-offline"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	HandleAPIChat(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["ok"] != true {
		t.Fatalf("%v", m)
	}
	if m["via"] != "local" {
		t.Fatalf("via=%v", m["via"])
	}
	reply, _ := m["reply"].(string)
	if !strings.Contains(strings.ToLower(reply), "sphere") && !strings.Contains(strings.ToLower(reply), "local") {
		t.Fatalf("reply=%q", reply)
	}
}

func TestSphereSessionTurns(t *testing.T) {
	s := getSphereSession("unit-sess")
	s.clear()
	s.appendTurn("hi", "hello there")
	h := s.snapHistory()
	if len(h) != 2 || h[0].Role != "user" || h[1].Role != "assistant" {
		t.Fatalf("%+v", h)
	}
}
