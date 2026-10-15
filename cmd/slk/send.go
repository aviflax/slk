package main

import "time"

// sendTimeout bounds a chat.postMessage call for a message or reply.
// The UI holds the user's own messages echoed in a conversation until
// every send pending there has a result, so a send must always end.
const sendTimeout = 30 * time.Second

// requestTimeout bounds any other Web API call the UI waits on: a
// history or thread fetch, a join, a reaction, a read mark. Its result
// clears a spinner or an in-flight flag, so it must always come.
const requestTimeout = 30 * time.Second
