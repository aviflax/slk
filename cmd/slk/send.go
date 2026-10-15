package main

import "time"

// sendTimeout bounds a chat.postMessage call for a message or reply.
// The UI holds the user's own messages echoed in a conversation until
// every send pending there has a result, so a send must always end.
const sendTimeout = 30 * time.Second
