package fts

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
	"github.com/wxbackup/wxbackup/testdata/fixtures"
)

func TestRebuildSearchPhrase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := seededConn(t)

	if err := Rebuild(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Rebuild(ctx, db); err != nil {
		t.Fatal(err)
	}

	got, err := Search(ctx, db, Query{Q: "hello fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].MsgID != "m1" || got[0].Text != "hello fixture" {
		t.Fatalf("phrase %+v", got)
	}

	later, err := Search(ctx, db, Query{Q: "later"})
	if err != nil {
		t.Fatal(err)
	}
	if ids := msgIDs(later); strings.Join(ids, ",") != "m5,g2" {
		t.Fatalf("later %v", ids)
	}
}

func TestSearchTypeAndDateFilters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := seededConn(t)
	if err := Rebuild(ctx, db); err != nil {
		t.Fatal(err)
	}

	text := fixtures.MsgText
	got, err := Search(ctx, db, Query{Q: "later", MsgType: &text})
	if err != nil {
		t.Fatal(err)
	}
	if ids := msgIDs(got); strings.Join(ids, ",") != "m5,g2" {
		t.Fatalf("type text %v", ids)
	}

	sys := fixtures.MsgSystem
	sysHits, err := Search(ctx, db, Query{Q: "synthetic", MsgType: &sys})
	if err != nil {
		t.Fatal(err)
	}
	if len(sysHits) != 1 || sysHits[0].MsgID != "m6" {
		t.Fatalf("system %+v", sysHits)
	}

	img := fixtures.MsgImage
	none, err := Search(ctx, db, Query{Q: "hello fixture", MsgType: &img})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("image filter %+v", none)
	}

	from := time.Date(2024, 1, 2, 3, 4, 4, 0, time.UTC)
	ranged, err := Search(ctx, db, Query{Q: "later", From: &from})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranged) != 1 || ranged[0].MsgID != "m5" {
		t.Fatalf("from %+v", ranged)
	}

	to := time.Date(2024, 1, 2, 3, 4, 3, 0, time.UTC)
	early, err := Search(ctx, db, Query{Q: "later", To: &to})
	if err != nil {
		t.Fatal(err)
	}
	if len(early) != 1 || early[0].MsgID != "g2" {
		t.Fatalf("to %+v", early)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := seededConn(t)
	if err := Rebuild(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"", "  ", "\t"} {
		got, err := Search(ctx, db, Query{Q: q, Limit: 10})
		if err != nil {
			t.Fatalf("q %q: %v", q, err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("q %q: %+v", q, got)
		}
	}
}

func TestSearchBackfillsWithoutRebuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := seededConn(t)
	got, err := Search(ctx, db, Query{Q: "hello fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].MsgID != "m1" {
		t.Fatalf("%+v", got)
	}
}

func TestRebuildAsync(t *testing.T) {
	t.Parallel()
	db := seededConn(t)
	if err := <-RebuildAsync(db); err != nil {
		t.Fatal(err)
	}
	got, err := Search(context.Background(), db, Query{Q: "group hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].MsgID != "g1" {
		t.Fatalf("%+v", got)
	}
}

func seededConn(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := fixtures.Seed(context.Background(), store, dir); err != nil {
		t.Fatal(err)
	}
	adb, err := store.OpenAccount(context.Background(), fixtures.WxID)
	if err != nil {
		t.Fatal(err)
	}
	return adb.Conn()
}

func msgIDs(msgs []domain.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.MsgID
	}
	return out
}
