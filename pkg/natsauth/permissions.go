package natsauth

import "time"

// ResponsePermissionTTL is the broker's window for one reply to a received request.
const ResponsePermissionTTL = 5 * time.Minute

// ResponsePermissionMargin reserves time for reply delivery after tool execution.
const ResponsePermissionMargin = 30 * time.Second
