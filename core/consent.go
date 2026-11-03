package core

import "fmt"

// SessionConsentMessage is what each player signs to join a session. It binds
// the chain (no cross-chain replay), the session, the game and the exact
// stakes. Strings are prefixed with their decimal byte length so IDs
// containing ':' cannot shift field boundaries:
//
//	session-consent:v2:12:tolchain-dev:6:sess-1:6:game-1:100
//
// Off-chain signers (e.g. custodial game backends) must reproduce these bytes
// exactly. The v1 format "session:<sessionID>" is no longer accepted.
func SessionConsentMessage(chainID, sessionID, gameID string, stakes uint64) []byte {
	return []byte(fmt.Sprintf("session-consent:v2:%d:%s:%d:%s:%d:%s:%d",
		len(chainID), chainID, len(sessionID), sessionID, len(gameID), gameID, stakes))
}
