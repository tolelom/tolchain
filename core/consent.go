package core

import "fmt"

// SessionConsentMessage returns the canonical byte string a player must sign
// to consent to joining a game session (SessionOpenPayload.Signatures).
//
// The signature commits to every field that affects what the player agrees to:
//   - chainID:   the network the consent is valid on (prevents cross-chain replay)
//   - sessionID: the specific session being opened
//   - gameID:    the game being played
//   - stakes:    the exact token amount debited from the player
//
// Canonical encoding (v2): each variable-length string field is prefixed with
// its byte length in decimal, so IDs containing ':' cannot shift field
// boundaries — no two distinct field tuples encode to the same byte string:
//
//	session-consent:v2:<len(chainID)>:<chainID>:<len(sessionID)>:<sessionID>:<len(gameID)>:<gameID>:<stakes>
//
// Example: SessionConsentMessage("tolchain-dev", "sess-1", "game-1", 100)
// produces "session-consent:v2:12:tolchain-dev:6:sess-1:6:game-1:100".
//
// The legacy v1 format ("session:<sessionID>") did not commit to chainID,
// stakes or gameID and is no longer accepted by the session module.
// Consent generators (e.g. the game backend holding custodial keys) must use
// this exact function — or reproduce these exact bytes — for signatures to
// verify on-chain.
func SessionConsentMessage(chainID, sessionID, gameID string, stakes uint64) []byte {
	return []byte(fmt.Sprintf("session-consent:v2:%d:%s:%d:%s:%d:%s:%d",
		len(chainID), chainID, len(sessionID), sessionID, len(gameID), gameID, stakes))
}
