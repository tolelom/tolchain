package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func main() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Println("키 생성 실패:", err)
		return
	}
	fmt.Println("OPERATOR_KEY_HEX=" + hex.EncodeToString(priv))
	fmt.Println("PUBLIC_KEY=" + hex.EncodeToString(pub))
}
