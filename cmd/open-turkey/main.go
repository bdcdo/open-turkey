// Package main é o ponto de entrada do Open Turkey — um bloqueador de
// produtividade para Linux via linha de comando (CLI).
//
// Por que o main.go é tão enxuto?
// ---------------------------------
// Seguimos o princípio de separação de responsabilidades (separation of
// concerns). O papel do main.go é exclusivamente inicializar a aplicação.
// Toda a lógica de comandos, flags e execução fica dentro dos pacotes
// internos (internal/), o que traz algumas vantagens:
//
//   - Testabilidade: pacotes internos podem ser testados de forma isolada,
//     sem precisar executar o binário inteiro.
//   - Organização: cada pacote cuida de uma única responsabilidade.
//   - Encapsulamento: o diretório internal/ garante que outros módulos
//     externos não importem nossa lógica interna por acidente.
//
// A função main() apenas delega a execução para cli.Execute(), que é
// responsável por configurar e rodar o comando raiz (root command) usando
// o padrão Cobra.
package main

import (
	"github.com/brunodcdo/open-turkey/internal/cli"
)

// main é o ponto de entrada da aplicação. Chamamos cli.Execute() que
// inicializa o comando raiz do Cobra e processa os argumentos da linha
// de comando. Se houver algum erro, o próprio Cobra cuida de exibir a
// mensagem de erro e encerrar o processo com código de saída apropriado.
func main() {
	cli.Execute()
}
