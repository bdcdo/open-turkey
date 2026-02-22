// Package cli define os comandos de linha de comando do Open Turkey usando Cobra.
//
// O Cobra é uma biblioteca popular em Go para criar CLIs. Ele organiza a aplicação
// em "comandos" (commands) que podem ter subcomandos, flags e argumentos.
// Por exemplo: "open-turkey block create redes-sociais" tem:
//   - "open-turkey" como comando raiz (root)
//   - "block" como subcomando
//   - "create" como sub-subcomando
//   - "redes-sociais" como argumento
//
// Este arquivo define o comando raiz e a função Execute() que inicia tudo.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd é o comando raiz da aplicação. Todos os outros comandos (block, session, etc.)
// são adicionados como filhos deste comando. Quando o usuário digita "open-turkey"
// sem argumentos, o Cobra exibe a ajuda deste comando automaticamente.
var rootCmd = &cobra.Command{
	Use:   "open-turkey",
	Short: "Bloqueador de produtividade para Linux via linha de comando",
}

// Execute inicializa e executa o comando raiz do Cobra.
// Esta função é chamada pelo main.go — é o ponto de entrada da CLI.
// Se qualquer comando retornar erro, imprimimos a mensagem no stderr
// e encerramos o processo com código de saída 1.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
		os.Exit(1)
	}
}
