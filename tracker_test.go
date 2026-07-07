package main

import (
	"testing"
	"time"
)

func TestTriggersOnThirdDistinctChannel(t *testing.T) {
	tr := newTracker(time.Minute, 3)
	now := time.Now()

	if _, act := tr.Record("u1", "img", "ch1", "m1", now); act {
		t.Fatal("1チャンネル目で発火してはいけない")
	}
	if _, act := tr.Record("u1", "img", "ch2", "m2", now.Add(10*time.Second)); act {
		t.Fatal("2チャンネル目で発火してはいけない")
	}
	refs, act := tr.Record("u1", "img", "ch3", "m3", now.Add(20*time.Second))
	if !act {
		t.Fatal("3チャンネル目で発火すべき")
	}
	if len(refs) != 3 {
		t.Fatalf("削除対象は3件のはず: got %d", len(refs))
	}
}

func TestSameChannelDoesNotTrigger(t *testing.T) {
	tr := newTracker(time.Minute, 3)
	now := time.Now()

	for i := 0; i < 10; i++ {
		if _, act := tr.Record("u1", "img", "ch1", "m", now.Add(time.Duration(i)*time.Second)); act {
			t.Fatal("同一チャンネル連投では発火してはいけない")
		}
	}
}

func TestOldEventsExpire(t *testing.T) {
	tr := newTracker(time.Minute, 3)
	now := time.Now()

	tr.Record("u1", "img", "ch1", "m1", now)
	tr.Record("u1", "img", "ch2", "m2", now.Add(5*time.Second))
	// 3件目はウィンドウ外(90秒後)なので過去2件は失効している
	if _, act := tr.Record("u1", "img", "ch3", "m3", now.Add(90*time.Second)); act {
		t.Fatal("ウィンドウ外のイベントを数えてはいけない")
	}
}

func TestTriggersOnlyOnce(t *testing.T) {
	tr := newTracker(time.Minute, 3)
	now := time.Now()

	tr.Record("u1", "img", "ch1", "m1", now)
	tr.Record("u1", "img", "ch2", "m2", now)
	if _, act := tr.Record("u1", "img", "ch3", "m3", now); !act {
		t.Fatal("3チャンネル目で発火すべき")
	}
	if _, act := tr.Record("u1", "img", "ch4", "m4", now.Add(time.Second)); act {
		t.Fatal("同一エントリで二重発火してはいけない")
	}
}

func TestDifferentUsersAreIndependent(t *testing.T) {
	tr := newTracker(time.Minute, 3)
	now := time.Now()

	tr.Record("u1", "img", "ch1", "m1", now)
	tr.Record("u2", "img", "ch2", "m2", now)
	if _, act := tr.Record("u3", "img", "ch3", "m3", now); act {
		t.Fatal("別ユーザーの投稿を合算してはいけない")
	}
}

func TestSweepRemovesExpiredEntries(t *testing.T) {
	tr := newTracker(time.Minute, 3)
	now := time.Now()

	tr.Record("u1", "img", "ch1", "m1", now)
	tr.Record("u2", "img", "ch1", "m2", now.Add(2*time.Minute))
	tr.Sweep(now.Add(2*time.Minute + time.Second))

	if len(tr.entries) != 1 {
		t.Fatalf("失効エントリが掃除されていない: got %d entries", len(tr.entries))
	}
	if _, ok := tr.entries["u2|img"]; !ok {
		t.Fatal("有効なエントリまで消えている")
	}
}
