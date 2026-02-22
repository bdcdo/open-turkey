// Programa temporário para normalizar domínios existentes no banco de dados.
// Corrige entradas como "https://polymarket.com/" → "polymarket.com".
package main

import (
	"fmt"
	"os"

	"github.com/brunodcdo/open-turkey/internal/blocker"
	"github.com/brunodcdo/open-turkey/internal/db"
)

func main() {
	database, err := db.OpenDB(db.DefaultDBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao abrir banco: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	sites, err := database.GetAllSitesRaw()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao buscar sites: %v\n", err)
		os.Exit(1)
	}

	for _, s := range sites {
		normalizado := blocker.NormalizarDominio(s.Domain)
		if normalizado != s.Domain {
			fmt.Printf("Normalizando: '%s' → '%s'\n", s.Domain, normalizado)
			if err := database.UpdateSiteDomain(s.ID, normalizado); err != nil {
				fmt.Fprintf(os.Stderr, "Erro ao atualizar site %d: %v\n", s.ID, err)
				os.Exit(1)
			}
		}
	}

	fmt.Println("Domínios normalizados com sucesso.")
}
