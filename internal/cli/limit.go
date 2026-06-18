package cli

import (
	"fmt"
	"time"

	"github.com/brunodcdo/open-turkey/internal/db"
	"github.com/spf13/cobra"
)

var limitCmd = &cobra.Command{
	Use:   "limit",
	Short: "Gerenciar limites diários automáticos",
}

var limitSetCmd = &cobra.Command{
	Use:   "set [bloco] --daily <duração>",
	Short: "Configurar limite diário de uso para um bloco",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		blockName := args[0]
		daily, err := cmd.Flags().GetString("daily")
		if err != nil {
			return fmt.Errorf("erro ao ler flag --daily: %w", err)
		}
		seconds, err := parseLimitDuration(daily)
		if err != nil {
			return err
		}

		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		detail, err := database.GetBlock(blockName)
		if err != nil {
			return err
		}
		if len(detail.Sites) == 0 {
			return fmt.Errorf("limite diário automático exige pelo menos um site no bloco '%s'", blockName)
		}

		if err := database.SetDailyLimit(blockName, seconds); err != nil {
			return err
		}
		if detail.Active {
			if err := reaplicarOuRemoverCamadas(database); err != nil {
				return err
			}
		}

		fmt.Printf("Limite diário do bloco '%s' configurado para %s.\n", blockName, formatSeconds(seconds))
		return nil
	},
}

var limitRemoveCmd = &cobra.Command{
	Use:   "remove [bloco]",
	Short: "Remover limite diário de um bloco",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		blockName := args[0]

		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		detail, err := database.GetBlock(blockName)
		if err != nil {
			return err
		}
		if err := database.RemoveDailyLimit(blockName); err != nil {
			return err
		}
		if detail.Active {
			if err := reaplicarOuRemoverCamadas(database); err != nil {
				return err
			}
		}

		fmt.Printf("Limite diário do bloco '%s' removido.\n", blockName)
		return nil
	},
}

var limitStatusCmd = &cobra.Command{
	Use:   "status [bloco]",
	Short: "Mostrar limites diários configurados",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		day := currentDayKey()
		if len(args) == 1 {
			status, err := database.GetLimitStatus(args[0], day)
			if err != nil {
				return err
			}
			printLimitStatus(*status)
			return nil
		}

		statuses, err := database.ListLimitStatuses(day)
		if err != nil {
			return err
		}
		if len(statuses) == 0 {
			fmt.Println("Nenhum limite diário configurado.")
			return nil
		}
		for _, status := range statuses {
			printLimitStatus(status)
		}
		return nil
	},
}

func parseLimitDuration(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("informe uma duração com --daily, por exemplo: --daily 30m")
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("duração inválida '%s': use formatos como 30m, 1h ou 1h30m", value)
	}
	seconds := int(duration.Seconds())
	if seconds <= 0 {
		return 0, fmt.Errorf("duração deve ser maior que zero")
	}
	return seconds, nil
}

func printLimitStatus(status db.LimitStatus) {
	if !status.HasDailyLimit {
		fmt.Printf("%s: sem limite diário configurado\n", status.BlockName)
		return
	}

	used := status.UsedSecondsToday
	if used > status.DailyLimitSeconds {
		used = status.DailyLimitSeconds
	}
	remaining := status.DailyLimitSeconds - used

	state := "inativo"
	if status.Active {
		if remaining <= 0 {
			state = "bloqueado até amanhã"
		} else {
			state = "monitorando"
		}
	}

	fmt.Printf(
		"%s: %s usados de %s (%s restantes) [%s]\n",
		status.BlockName,
		formatSeconds(used),
		formatSeconds(status.DailyLimitSeconds),
		formatSeconds(remaining),
		state,
	)
}

func formatSeconds(seconds int) string {
	if seconds <= 0 {
		return "0s"
	}

	duration := time.Duration(seconds) * time.Second
	hours := duration / time.Hour
	duration -= hours * time.Hour
	minutes := duration / time.Minute
	duration -= minutes * time.Minute
	secs := duration / time.Second

	result := ""
	if hours > 0 {
		result += fmt.Sprintf("%dh", hours)
	}
	if minutes > 0 {
		result += fmt.Sprintf("%dm", minutes)
	}
	if secs > 0 || result == "" {
		result += fmt.Sprintf("%ds", secs)
	}
	return result
}

func currentDayKey() string {
	return time.Now().Local().Format("2006-01-02")
}

func init() {
	limitSetCmd.Flags().String("daily", "", "Cota diária de uso (ex: 30m, 1h, 1h30m)")
	limitCmd.AddCommand(limitSetCmd)
	limitCmd.AddCommand(limitRemoveCmd)
	limitCmd.AddCommand(limitStatusCmd)
	rootCmd.AddCommand(limitCmd)
}
