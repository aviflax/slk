package ui

import tea "charm.land/bubbletea/v2"

// tick schedules fn after d. Every timer the App arms goes through it
// so that the package's tests, which replace it in TestMain, never wait
// on a real clock. Production always runs tea.Tick.
var tick = tea.Tick
