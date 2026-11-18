// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr49

import "crypto/rand"

// cryptoRandRead reads len(p) random bytes from crypto/rand. Best-effort:
// callers (UUID generation, stream-name tags) tolerate the rare error
// path because the bytes still pass through and tests that produce
// identical bytes simply collide on stream name, which the next sweep
// clears anyway.
func cryptoRandRead(p []byte) (int, error) {
	return rand.Read(p)
}