package tools

import (
	"fmt"
	"sync"
	"time"

	"github.com/zeebo/xxh3"
)

// maxLedgerSessions bounds the sessions a fileLedger remembers; the one used
// longest ago is forgotten first.
const maxLedgerSessions = 256

// fileLedger remembers which version of each file a session has read or
// written, so write, edit and multiedit refuse an existing file the session
// has not seen or that changed since. It lives in memory: after a daemon
// restart a file is read again before it is changed. Calls without a session
// (none in runs) are not checked.
type fileLedger struct {
	mu       sync.Mutex
	clock    uint64
	sessions map[string]*seenFiles
}

type seenFiles struct {
	used  uint64
	files map[string]fileVersion
}

// fileVersion identifies a file's content; at is when the session saw it.
type fileVersion struct {
	size int64
	sum  uint64
	at   time.Time
}

func newFileLedger() *fileLedger {
	return &fileLedger{sessions: map[string]*seenFiles{}}
}

func versionOf(content []byte) fileVersion {
	return fileVersion{size: int64(len(content)), sum: xxh3.Hash(content), at: time.Now()}
}

// saw records that session has seen the file at path (a real path) as v.
func (l *fileLedger) saw(session string, path string, v fileVersion) {
	if l == nil || session == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clock++
	seen := l.sessions[session]
	if seen == nil {
		if len(l.sessions) >= maxLedgerSessions {
			l.forgetOldest()
		}
		seen = &seenFiles{files: map[string]fileVersion{}}
		l.sessions[session] = seen
	}
	seen.used = l.clock
	seen.files[path] = v
}

// check reports why session may not change the existing file at path, whose
// content is now content; nil when it saw exactly this content.
func (l *fileLedger) check(session string, path string, content []byte) error {
	if l == nil || session == "" {
		return nil
	}
	l.mu.Lock()
	seen := l.sessions[session]
	var v fileVersion
	var ok bool
	if seen != nil {
		v, ok = seen.files[path]
	}
	l.mu.Unlock()
	switch {
	case !ok:
		return fmt.Errorf("%s has not been read in this session; read it before changing it", path)
	case v.size != int64(len(content)) || v.sum != xxh3.Hash(content):
		return fmt.Errorf("%s changed after you last read it at %s (edited by the user, a formatter or another agent); read it again before changing it", path, v.at.Format(time.TimeOnly))
	}
	return nil
}

func (l *fileLedger) forgetOldest() {
	oldest := ""
	for id, seen := range l.sessions {
		if oldest == "" || seen.used < l.sessions[oldest].used {
			oldest = id
		}
	}
	delete(l.sessions, oldest)
}
