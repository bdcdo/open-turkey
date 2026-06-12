package enforcer

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/brunodcdo/open-turkey/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()

	database, err := db.OpenDB(filepath.Join(t.TempDir(), "open-turkey-test.db"))
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

func createLimitedActiveBlock(t *testing.T, database *db.DB, limitSeconds int) int {
	t.Helper()

	if err := database.CreateBlock("video"); err != nil {
		t.Fatalf("CreateBlock() error = %v", err)
	}
	if err := database.AddSites("video", []string{"video.example"}); err != nil {
		t.Fatalf("AddSites() error = %v", err)
	}
	if err := database.SetDailyLimit("video", limitSeconds); err != nil {
		t.Fatalf("SetDailyLimit() error = %v", err)
	}
	if err := database.ActivateBlock("video", false, 0); err != nil {
		t.Fatalf("ActivateBlock() error = %v", err)
	}

	detail, err := database.GetBlock("video")
	if err != nil {
		t.Fatalf("GetBlock() error = %v", err)
	}
	return detail.ID
}

func TestAccountLimitUsageCountsActiveWindow(t *testing.T) {
	limitUsageStates = make(map[int]*limitUsageState)
	database := openTestDB(t)
	blockID := createLimitedActiveBlock(t, database, 120)
	now := time.Date(2026, 6, 11, 10, 0, 0, 0, time.Local)
	day := dayKey(now)

	blocks, err := database.GetActiveBlocks(day)
	if err != nil {
		t.Fatalf("GetActiveBlocks() error = %v", err)
	}
	if err := accountLimitUsage(database, blocks, map[int]uint64{blockID: 1}, now, day); err != nil {
		t.Fatalf("accountLimitUsage(first) error = %v", err)
	}

	blocks, err = database.GetActiveBlocks(day)
	if err != nil {
		t.Fatalf("GetActiveBlocks(second) error = %v", err)
	}
	if err := accountLimitUsage(database, blocks, nil, now.Add(5*time.Second), day); err != nil {
		t.Fatalf("accountLimitUsage(second) error = %v", err)
	}

	blocks, err = database.GetActiveBlocks(day)
	if err != nil {
		t.Fatalf("GetActiveBlocks(third) error = %v", err)
	}
	if err := accountLimitUsage(database, blocks, nil, now.Add(70*time.Second), day); err != nil {
		t.Fatalf("accountLimitUsage(third) error = %v", err)
	}

	status, err := database.GetLimitStatus("video", day)
	if err != nil {
		t.Fatalf("GetLimitStatus() error = %v", err)
	}
	if status.UsedSecondsToday != 60 {
		t.Fatalf("expected 60 seconds used, got %d", status.UsedSecondsToday)
	}
}

func TestAccountLimitUsageCapsAtDailyLimit(t *testing.T) {
	limitUsageStates = make(map[int]*limitUsageState)
	database := openTestDB(t)
	blockID := createLimitedActiveBlock(t, database, 10)
	now := time.Date(2026, 6, 11, 10, 0, 0, 0, time.Local)
	day := dayKey(now)

	blocks, err := database.GetActiveBlocks(day)
	if err != nil {
		t.Fatalf("GetActiveBlocks() error = %v", err)
	}
	if err := accountLimitUsage(database, blocks, map[int]uint64{blockID: 1}, now, day); err != nil {
		t.Fatalf("accountLimitUsage(first) error = %v", err)
	}

	blocks, err = database.GetActiveBlocks(day)
	if err != nil {
		t.Fatalf("GetActiveBlocks(second) error = %v", err)
	}
	if err := accountLimitUsage(database, blocks, nil, now.Add(20*time.Second), day); err != nil {
		t.Fatalf("accountLimitUsage(second) error = %v", err)
	}

	status, err := database.GetLimitStatus("video", day)
	if err != nil {
		t.Fatalf("GetLimitStatus() error = %v", err)
	}
	if status.UsedSecondsToday != 10 {
		t.Fatalf("expected usage capped at 10 seconds, got %d", status.UsedSecondsToday)
	}
}
