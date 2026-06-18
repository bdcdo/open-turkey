package enforcer

import (
	"fmt"
	"time"

	"github.com/brunodcdo/open-turkey/internal/blocker"
	"github.com/brunodcdo/open-turkey/internal/db"
)

const activityWindow = 60 * time.Second

type limitUsageState struct {
	day             string
	activeUntil     time.Time
	lastAccountedAt time.Time
}

var limitUsageStates = make(map[int]*limitUsageState)

// AccountAndApply registra uso detectado nas chains de contagem e aplica a política atual.
func AccountAndApply(database *db.DB, now time.Time) error {
	day := dayKey(now)

	counters, err := blocker.ReadLimitTrackingCounters()
	if err != nil {
		return fmt.Errorf("erro ao ler contadores de limite diário: %w", err)
	}

	activeBlocks, err := database.GetActiveBlocks(day)
	if err != nil {
		return err
	}

	if err := accountLimitUsage(database, activeBlocks, counters, now, day); err != nil {
		return err
	}

	return Apply(database, now)
}

// Apply aplica bloqueios e tracking sem registrar novo uso.
func Apply(database *db.DB, now time.Time) error {
	activeBlocks, err := database.GetActiveBlocks(dayKey(now))
	if err != nil {
		return err
	}

	blockedDomains, blockedApps, trackTargets := classify(activeBlocks)
	return applyLayers(blockedDomains, blockedApps, trackTargets)
}

func accountLimitUsage(database *db.DB, activeBlocks []db.ActiveBlockDetail, counters map[int]uint64, now time.Time, day string) error {
	activeLimited := make(map[int]bool)

	for _, block := range activeBlocks {
		if !block.HasDailyLimit {
			continue
		}
		activeLimited[block.ID] = true

		remaining := block.DailyLimitSeconds - block.UsedSecondsToday
		if remaining <= 0 {
			continue
		}

		state := limitUsageStates[block.ID]
		if state == nil || state.day != day {
			state = &limitUsageState{day: day, lastAccountedAt: now}
			limitUsageStates[block.ID] = state
		}

		accountUntil := now
		if state.activeUntil.Before(accountUntil) {
			accountUntil = state.activeUntil
		}
		if accountUntil.After(state.lastAccountedAt) {
			seconds := int(accountUntil.Sub(state.lastAccountedAt).Seconds())
			if seconds > remaining {
				seconds = remaining
			}
			if seconds > 0 {
				if err := database.AddDailyUsage(block.ID, day, seconds); err != nil {
					return err
				}
				remaining -= seconds
			}
		}

		state.lastAccountedAt = now
		if counters[block.ID] > 0 && remaining > 0 {
			activeUntil := now.Add(activityWindow)
			if activeUntil.After(state.activeUntil) {
				state.activeUntil = activeUntil
			}
		}
	}

	for blockID := range limitUsageStates {
		if !activeLimited[blockID] {
			delete(limitUsageStates, blockID)
		}
	}

	return nil
}

func classify(activeBlocks []db.ActiveBlockDetail) ([]string, []string, []blocker.LimitTrackTarget) {
	var blockedDomains []string
	var blockedApps []string
	var trackTargets []blocker.LimitTrackTarget

	for _, block := range activeBlocks {
		if block.HasDailyLimit && block.UsedSecondsToday < block.DailyLimitSeconds {
			if len(block.Sites) > 0 {
				trackTargets = append(trackTargets, blocker.LimitTrackTarget{
					BlockID: block.ID,
					Domains: block.Sites,
				})
			}
			continue
		}

		blockedDomains = append(blockedDomains, block.Sites...)
		blockedApps = append(blockedApps, block.Apps...)
	}

	return blockedDomains, blockedApps, trackTargets
}

func applyLayers(blockedDomains []string, blockedApps []string, trackTargets []blocker.LimitTrackTarget) error {
	if len(blockedDomains) > 0 {
		if err := blocker.ApplyHosts(blockedDomains); err != nil {
			return fmt.Errorf("erro ao aplicar bloqueio no /etc/hosts: %w", err)
		}
		if err := blocker.ApplyFirewall(blockedDomains); err != nil {
			return fmt.Errorf("erro ao aplicar bloqueio no firewall: %w", err)
		}
		if err := blocker.ApplyBrowserPolicies(blockedDomains); err != nil {
			return fmt.Errorf("erro ao aplicar políticas de navegador: %w", err)
		}
	} else {
		if err := blocker.RemoveHosts(); err != nil {
			return fmt.Errorf("erro ao remover bloqueio do /etc/hosts: %w", err)
		}
		if err := blocker.RemoveFirewall(); err != nil {
			return fmt.Errorf("erro ao remover bloqueio do firewall: %w", err)
		}
		if err := blocker.RemoveBrowserPolicies(); err != nil {
			return fmt.Errorf("erro ao remover políticas de navegador: %w", err)
		}
	}

	if len(trackTargets) > 0 {
		if err := blocker.ApplyLimitTracking(trackTargets); err != nil {
			return fmt.Errorf("erro ao aplicar tracking de limite diário: %w", err)
		}
	} else if err := blocker.RemoveLimitTracking(); err != nil {
		return fmt.Errorf("erro ao remover tracking de limite diário: %w", err)
	}

	if len(blockedApps) > 0 {
		if _, err := blocker.KillBlocked(blockedApps); err != nil {
			return fmt.Errorf("erro ao matar processos bloqueados: %w", err)
		}
	}

	return nil
}

func dayKey(t time.Time) string {
	return t.Local().Format("2006-01-02")
}
