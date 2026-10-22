package token

import "time"

func (v *Verifier) SetUnknownKidInterval(d time.Duration) { v.unknownKidInterval = d }

func (v *Verifier) SetWarmDelays(initial, max time.Duration) {
	v.warmDelay, v.warmMaxDelay = initial, max
}
