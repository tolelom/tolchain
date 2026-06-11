package core

import (
	"bytes"
	"testing"
)

// 정식 포맷이 정확히 고정되어 있는지 검증한다. a301_server 등 외부 컨슈머가
// 같은 바이트열을 생성해야 하므로 리터럴로 핀(pin)한다.
func TestSessionConsentMessage_CanonicalFormat(t *testing.T) {
	got := SessionConsentMessage("tolchain-dev", "sess-1", "game-1", 100)
	want := "session-consent:v2:12:tolchain-dev:6:sess-1:6:game-1:100"
	if string(got) != want {
		t.Fatalf("SessionConsentMessage = %q, want %q", got, want)
	}
}

func TestSessionConsentMessage_EmptyFields(t *testing.T) {
	got := SessionConsentMessage("", "", "", 0)
	want := "session-consent:v2:0::0::0::0"
	if string(got) != want {
		t.Fatalf("SessionConsentMessage = %q, want %q", got, want)
	}
}

// 결정에 영향을 주는 4개 필드 중 하나라도 바뀌면 메시지가 달라져야 한다.
func TestSessionConsentMessage_FieldChangesChangeMessage(t *testing.T) {
	base := SessionConsentMessage("c", "s", "g", 1)
	variants := map[string][]byte{
		"chain_id":   SessionConsentMessage("c2", "s", "g", 1),
		"session_id": SessionConsentMessage("c", "s2", "g", 1),
		"game_id":    SessionConsentMessage("c", "s", "g2", 1),
		"stakes":     SessionConsentMessage("c", "s", "g", 2),
	}
	for name, v := range variants {
		if bytes.Equal(base, v) {
			t.Errorf("changing %s must change the consent message", name)
		}
	}
}

// 길이 프리픽스 인코딩 덕분에 ':'가 포함된 ID가 필드 경계를 이동시켜
// 서로 다른 필드 조합이 같은 바이트열로 인코딩되는 일이 없어야 한다.
// (naive한 콜론 join이라면 아래 두 조합은 동일한 문자열이 된다.)
func TestSessionConsentMessage_NoBoundaryAmbiguity(t *testing.T) {
	a := SessionConsentMessage("chain", "s1:100", "g", 5)
	b := SessionConsentMessage("chain", "s1", "100:g", 5)
	if bytes.Equal(a, b) {
		t.Fatalf("field-boundary collision: %q", a)
	}
}
