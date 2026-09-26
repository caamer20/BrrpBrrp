package api

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"time"
)

// RFC 6238, SHA-1, six digits and a thirty-second step. The adjacent
// steps tolerate clock drift; callers persist the accepted counter.
func totpCode(secret string, counter int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || len(key) < 20 || counter < 0 {
		return ""
	}
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	number := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", number%1000000)
}

func matchTOTP(secret, code string, now time.Time, last int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, offset := range []int64{0, -1, 1} {
		counter := now.Unix()/30 + offset
		if counter > last && subtle.ConstantTimeCompare([]byte(totpCode(secret, counter)), []byte(code)) == 1 {
			return counter, true
		}
	}
	return 0, false
}
