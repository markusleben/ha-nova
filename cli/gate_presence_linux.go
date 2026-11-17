package main

import "time"

// Linux: no idle reading without a new dependency; the App times the phone
// for "unknown".
func nativeIdleTime() (time.Duration, bool) { return 0, false }
