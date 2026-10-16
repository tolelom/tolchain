package core

import (
	"bytes"
	"testing"
)

func baseConsent() SessionConsent {
	return SessionConsent{ChainID: "c", SessionID: "s", GameID: "g", Stakes: 1, Creator: "op", Players: []string{"p1", "p2"}}
}

// 정식 포맷이 정확히 고정되어 있는지 검증한다. 동의 서명을 만드는 쪽이
// 같은 바이트열을 생성해야 하므로 리터럴로 핀(pin)한다.
func TestSessionConsent_CanonicalFormat(t *testing.T) {
	got := SessionConsent{
		ChainID: "tolchain-dev", SessionID: "sess-1", GameID: "game-1", Stakes: 100,
		Creator: "op", Players: []string{"bb", "aa"},
	}.Message()
	want := "session-consent:v3:12:tolchain-dev:6:sess-1:6:game-1:100:2:op:2:2:aa:2:bb"
	if string(got) != want {
		t.Fatalf("Message = %q, want %q", got, want)
	}
}

// 플레이어가 동의하는 대상에 영향을 주는 필드 중 하나라도 바뀌면 메시지가 달라져야 한다.
func TestSessionConsent_FieldChangesChangeMessage(t *testing.T) {
	base := baseConsent().Message()
	variants := map[string]func(*SessionConsent){
		"chain_id":   func(c *SessionConsent) { c.ChainID = "c2" },
		"session_id": func(c *SessionConsent) { c.SessionID = "s2" },
		"game_id":    func(c *SessionConsent) { c.GameID = "g2" },
		"stakes":     func(c *SessionConsent) { c.Stakes = 2 },
		"creator":    func(c *SessionConsent) { c.Creator = "op2" },
		"players":    func(c *SessionConsent) { c.Players = []string{"p1", "p3"} },
	}
	for name, mutate := range variants {
		c := baseConsent()
		mutate(&c)
		if bytes.Equal(base, c.Message()) {
			t.Errorf("changing %s must change the consent message", name)
		}
	}
}

func TestSessionConsent_PlayerOrderDoesNotMatter(t *testing.T) {
	a := baseConsent()
	b := baseConsent()
	b.Players = []string{"p2", "p1"}
	if !bytes.Equal(a.Message(), b.Message()) {
		t.Fatal("players sign the set of opponents, not the payload order")
	}
	if b.Players[0] != "p2" {
		t.Fatal("Message must not reorder the caller's slice")
	}
}

// 길이 프리픽스 인코딩 덕분에 ':'가 포함된 ID가 필드 경계를 이동시켜
// 서로 다른 필드 조합이 같은 바이트열로 인코딩되는 일이 없어야 한다.
func TestSessionConsent_NoBoundaryAmbiguity(t *testing.T) {
	a := baseConsent()
	a.SessionID, a.GameID = "s1:100", "g"
	b := baseConsent()
	b.SessionID, b.GameID = "s1", "100:g"
	if bytes.Equal(a.Message(), b.Message()) {
		t.Fatalf("field-boundary collision: %q", a.Message())
	}
}
