package db

import (
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()

	database, err := OpenDB(filepath.Join(t.TempDir(), "open-turkey-test.db"))
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return database
}

func createSiteBlock(t *testing.T, database *DB, name string) int {
	t.Helper()

	if err := database.CreateBlock(name); err != nil {
		t.Fatalf("CreateBlock() error = %v", err)
	}
	if err := database.AddSites(name, []string{"example.com"}); err != nil {
		t.Fatalf("AddSites() error = %v", err)
	}
	detail, err := database.GetBlock(name)
	if err != nil {
		t.Fatalf("GetBlock() error = %v", err)
	}
	return detail.ID
}

func TestDailyLimitStatusResetsByDay(t *testing.T) {
	database := openTestDB(t)
	blockID := createSiteBlock(t, database, "focus")

	if err := database.SetDailyLimit("focus", 1800); err != nil {
		t.Fatalf("SetDailyLimit() error = %v", err)
	}
	if err := database.AddDailyUsage(blockID, "2026-06-11", 600); err != nil {
		t.Fatalf("AddDailyUsage() error = %v", err)
	}

	status, err := database.GetLimitStatus("focus", "2026-06-11")
	if err != nil {
		t.Fatalf("GetLimitStatus() error = %v", err)
	}
	if !status.HasDailyLimit || status.DailyLimitSeconds != 1800 || status.UsedSecondsToday != 600 {
		t.Fatalf("unexpected status for first day: %+v", status)
	}

	nextDay, err := database.GetLimitStatus("focus", "2026-06-12")
	if err != nil {
		t.Fatalf("GetLimitStatus(next day) error = %v", err)
	}
	if nextDay.UsedSecondsToday != 0 {
		t.Fatalf("expected next day usage to reset, got %d", nextDay.UsedSecondsToday)
	}
}

func TestGetActiveBlocksIncludesDailyLimit(t *testing.T) {
	database := openTestDB(t)
	blockID := createSiteBlock(t, database, "news")

	if err := database.SetDailyLimit("news", 900); err != nil {
		t.Fatalf("SetDailyLimit() error = %v", err)
	}
	if err := database.AddDailyUsage(blockID, "2026-06-11", 300); err != nil {
		t.Fatalf("AddDailyUsage() error = %v", err)
	}
	if err := database.ActivateBlock("news", true, 300); err != nil {
		t.Fatalf("ActivateBlock() error = %v", err)
	}

	blocks, err := database.GetActiveBlocks("2026-06-11")
	if err != nil {
		t.Fatalf("GetActiveBlocks() error = %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 active block, got %d", len(blocks))
	}
	block := blocks[0]
	if !block.HasDailyLimit || block.DailyLimitSeconds != 900 || block.UsedSecondsToday != 300 {
		t.Fatalf("unexpected active block limit data: %+v", block)
	}
	if !block.Locked || block.LockChars != 300 {
		t.Fatalf("expected lock metadata to be preserved: %+v", block)
	}
}

func TestRemoveDailyLimit(t *testing.T) {
	database := openTestDB(t)
	createSiteBlock(t, database, "social")

	if err := database.SetDailyLimit("social", 600); err != nil {
		t.Fatalf("SetDailyLimit() error = %v", err)
	}
	if err := database.RemoveDailyLimit("social"); err != nil {
		t.Fatalf("RemoveDailyLimit() error = %v", err)
	}

	status, err := database.GetLimitStatus("social", "2026-06-11")
	if err != nil {
		t.Fatalf("GetLimitStatus() error = %v", err)
	}
	if status.HasDailyLimit {
		t.Fatalf("expected removed limit, got %+v", status)
	}
}
