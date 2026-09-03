// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr42

import "crypto/rand"

// cryptoRandRead reads len(p) random bytes from crypto/rand.
func cryptoRandRead(p []byte) (int, error) {
	return rand.Read(p)
}
