package main

import (
	"sync"
	"time"
)

type msgRef struct {
	ChannelID string
	MessageID string
}

type event struct {
	ref msgRef
	at  time.Time
}

type entry struct {
	events []event
	acted  bool
}

// Tracker は「ユーザー×画像」ごとに直近 window 内の投稿を記録し、
// 異なるチャンネル数が threshold に達した瞬間を検出する。
type Tracker struct {
	mu        sync.Mutex
	window    time.Duration
	threshold int
	entries   map[string]*entry // key: userID + "|" + imageKey
}

func newTracker(window time.Duration, threshold int) *Tracker {
	return &Tracker{
		window:    window,
		threshold: threshold,
		entries:   make(map[string]*entry),
	}
}

// Record は投稿を1件記録し、この投稿で初めて閾値を超えた場合に
// (削除対象のメッセージ一覧, true) を返す。同一エントリで2回目以降は発火しない。
func (t *Tracker) Record(userID, imageKey, channelID, messageID string, now time.Time) ([]msgRef, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := userID + "|" + imageKey
	e := t.entries[key]
	if e == nil {
		e = &entry{}
		t.entries[key] = e
	}

	cutoff := now.Add(-t.window)
	kept := e.events[:0]
	for _, ev := range e.events {
		if ev.at.After(cutoff) {
			kept = append(kept, ev)
		}
	}
	e.events = append(kept, event{ref: msgRef{channelID, messageID}, at: now})

	if e.acted {
		return nil, false
	}

	channels := make(map[string]struct{}, len(e.events))
	for _, ev := range e.events {
		channels[ev.ref.ChannelID] = struct{}{}
	}
	if len(channels) < t.threshold {
		return nil, false
	}

	e.acted = true
	refs := make([]msgRef, len(e.events))
	for i, ev := range e.events {
		refs[i] = ev.ref
	}
	return refs, true
}

// Sweep は window より古いイベントしか持たないエントリを削除する。
func (t *Tracker) Sweep(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := now.Add(-t.window)
	for key, e := range t.entries {
		expired := true
		for _, ev := range e.events {
			if ev.at.After(cutoff) {
				expired = false
				break
			}
		}
		if expired {
			delete(t.entries, key)
		}
	}
}

func (t *Tracker) startSweeper(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for now := range ticker.C {
			t.Sweep(now)
		}
	}()
}
