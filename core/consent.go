package core

import (
	"fmt"
	"slices"
	"strings"
)

// SessionConsent is what a player agrees to by signing a session_open
// consent (SessionOpenPayload.Signatures):
//   - ChainID:   the network the consent is valid on (prevents cross-chain replay)
//   - SessionID: the specific session being opened
//   - GameID:    the game being played
//   - Stakes:    the exact token amount debited from the player
//   - Creator:   the account that opens the session and alone submits its
//     result (the tx's effective sender), so another operator cannot open it
//   - Players:   the full set of participants, compared regardless of order
type SessionConsent struct {
	ChainID   string
	SessionID string
	GameID    string
	Stakes    uint64
	Creator   string
	Players   []string
}

// Message returns the canonical byte string a player signs.
//
// Canonical encoding (v3): each variable-length string is prefixed with its
// byte length in decimal, so values containing ':' cannot shift field
// boundaries. Players are sorted and preceded by their count:
//
//	session-consent:v3:<len>:<chainID>:<len>:<sessionID>:<len>:<gameID>:<stakes>:<len>:<creator>:<n>(:<len>:<player>)*
//
// Example: SessionConsent{"tolchain-dev", "sess-1", "game-1", 100, "op", []string{"bb", "aa"}}
// produces "session-consent:v3:12:tolchain-dev:6:sess-1:6:game-1:100:2:op:2:2:aa:2:bb".
//
// Earlier formats (v1 "session:<id>", v2 without creator and players) are no
// longer accepted. Consent generators must use this method — or reproduce
// these exact bytes — for signatures to verify on-chain.
func (c SessionConsent) Message() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "session-consent:v3:%d:%s:%d:%s:%d:%s:%d:%d:%s:%d",
		len(c.ChainID), c.ChainID, len(c.SessionID), c.SessionID, len(c.GameID), c.GameID,
		c.Stakes, len(c.Creator), c.Creator, len(c.Players))
	for _, p := range slices.Sorted(slices.Values(c.Players)) {
		fmt.Fprintf(&b, ":%d:%s", len(p), p)
	}
	return []byte(b.String())
}
