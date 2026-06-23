// Package id generates ULIDs: 128-bit, lexicographically sortable,
// time-ordered identifiers. Sorting ULIDs as strings orders them by
// creation time, which lets the journal rely on ID order as a tiebreaker.
//
// See https://github.com/ulid/spec for the format.
package id

import (
	"crypto/rand"
	"fmt"
	"io"
	"time"
)

// crockford is the Crockford base32 alphabet used by the ULID spec
// (excludes I, L, O, U to avoid ambiguity).
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// encodedLen is the fixed length of a ULID string.
const encodedLen = 26

// New returns a ULID for the current time using crypto/rand for entropy.
// It panics only if the system random source is unreadable, which Go
// treats as a fatal condition.
func New() string {
	s, err := NewAt(time.Now(), rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("id: generating ULID: %v", err))
	}
	return s
}

// NewAt returns a ULID encoding t (millisecond precision) with 80 bits of
// entropy drawn from r. It is the testable core of New.
func NewAt(t time.Time, r io.Reader) (string, error) {
	var b [16]byte

	ms := uint64(t.UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)

	if _, err := io.ReadFull(r, b[6:]); err != nil {
		return "", fmt.Errorf("read entropy: %w", err)
	}

	return encode(b), nil
}

// encode renders the 128-bit value as 26 Crockford base32 characters.
func encode(b [16]byte) string {
	var out [encodedLen]byte

	// 48-bit timestamp -> first 10 characters.
	out[0] = crockford[(b[0]&224)>>5]
	out[1] = crockford[b[0]&31]
	out[2] = crockford[(b[1]&248)>>3]
	out[3] = crockford[((b[1]&7)<<2)|((b[2]&192)>>6)]
	out[4] = crockford[(b[2]&62)>>1]
	out[5] = crockford[((b[2]&1)<<4)|((b[3]&240)>>4)]
	out[6] = crockford[((b[3]&15)<<1)|((b[4]&128)>>7)]
	out[7] = crockford[(b[4]&124)>>2]
	out[8] = crockford[((b[4]&3)<<3)|((b[5]&224)>>5)]
	out[9] = crockford[b[5]&31]

	// 80-bit entropy -> remaining 16 characters.
	out[10] = crockford[(b[6]&248)>>3]
	out[11] = crockford[((b[6]&7)<<2)|((b[7]&192)>>6)]
	out[12] = crockford[(b[7]&62)>>1]
	out[13] = crockford[((b[7]&1)<<4)|((b[8]&240)>>4)]
	out[14] = crockford[((b[8]&15)<<1)|((b[9]&128)>>7)]
	out[15] = crockford[(b[9]&124)>>2]
	out[16] = crockford[((b[9]&3)<<3)|((b[10]&224)>>5)]
	out[17] = crockford[b[10]&31]
	out[18] = crockford[(b[11]&248)>>3]
	out[19] = crockford[((b[11]&7)<<2)|((b[12]&192)>>6)]
	out[20] = crockford[(b[12]&62)>>1]
	out[21] = crockford[((b[12]&1)<<4)|((b[13]&240)>>4)]
	out[22] = crockford[((b[13]&15)<<1)|((b[14]&128)>>7)]
	out[23] = crockford[(b[14]&124)>>2]
	out[24] = crockford[((b[14]&3)<<3)|((b[15]&224)>>5)]
	out[25] = crockford[b[15]&31]

	return string(out[:])
}
