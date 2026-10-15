package main

import "time"

// sendTimeout bounds a chat.postMessage call for a message or reply.
// The UI holds the user's own messages echoed in a conversation until
// every send pending there has a result, so a send must always end.
const sendTimeout = 30 * time.Second

// requestTimeout bounds any other Web API call the UI waits on: a
// history fetch, a join, a reaction, a read mark. Its result clears a
// spinner or an in-flight flag, so it must always come. A call that
// pages (a thread's replies) is not given it: the client bounds each
// page's request instead, so the call ends however many pages it takes.
const requestTimeout = 30 * time.Second
