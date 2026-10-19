// Command genkeys prints an Ed25519 JWK set for a local boot, so the baseline in
// docs/engineering/performance-baseline.md can be reproduced without a signing
// key checked into the repository.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"

	"github.com/lestrrat-go/jwx/v3/jwk"
)

func main() {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "genkeys:", err)
		os.Exit(1)
	}
	key, err := jwk.Import(privateKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "genkeys:", err)
		os.Exit(1)
	}
	if err := key.Set(jwk.KeyIDKey, "local-kid"); err != nil {
		fmt.Fprintln(os.Stderr, "genkeys:", err)
		os.Exit(1)
	}
	set := jwk.NewSet()
	if err := set.AddKey(key); err != nil {
		fmt.Fprintln(os.Stderr, "genkeys:", err)
		os.Exit(1)
	}
	raw, err := json.Marshal(set)
	if err != nil {
		fmt.Fprintln(os.Stderr, "genkeys:", err)
		os.Exit(1)
	}
	fmt.Print(string(raw))
}
