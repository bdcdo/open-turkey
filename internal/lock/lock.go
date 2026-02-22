// Package lock implementa o mecanismo de "desafio de digitação" do Open Turkey.
//
// === O QUE É ISSO? ===
//
// Este pacote é a ÚNICA forma de desbloquear um bloqueio ativo no Open Turkey.
// A ideia é simples: criar "fricção". Quando você está tentado a acessar algo
// bloqueado (redes sociais, jogos, etc.), o sistema te obriga a digitar uma
// string aleatória enorme — caractere por caractere, sem erro.
//
// === POR QUE ISSO FUNCIONA? ===
//
// Não é segurança de verdade. Qualquer pessoa com acesso ao código poderia
// burlar isso. O ponto é PSICOLÓGICO: a tarefa chata de digitar centenas de
// caracteres aleatórios te dá tempo para repensar se realmente precisa
// desbloquear. Na maioria das vezes, você desiste — e era isso que queria.
//
// === POR QUE crypto/rand E NÃO math/rand? ===
//
// math/rand gera números "pseudo-aleatórios" — parecem aleatórios, mas seguem
// um padrão previsível se você souber a "semente" (seed). Alguém esperto
// poderia prever a string gerada e colar sem digitar.
//
// crypto/rand usa fontes de aleatoriedade do sistema operacional (como /dev/urandom
// no Linux). É verdadeiramente imprevisível — ninguém consegue adivinhar a
// string antes dela ser gerada. Para o nosso caso, é um exagero? Talvez.
// Mas é a coisa certa a fazer, e o custo é praticamente zero.
package lock

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"strings"
)

// DefaultLockChars é a quantidade padrão de caracteres do desafio.
// 300 caracteres é o suficiente para ser bem chato de digitar, mas não
// impossível. Leva uns 3-5 minutos para a maioria das pessoas — tempo
// suficiente para o impulso de "preciso ver o Instagram AGORA" passar.
const DefaultLockChars = 300

// charset é o conjunto de caracteres usados para gerar o desafio.
// Inclui letras minúsculas, maiúsculas, números e símbolos.
// A mistura de tipos de caractere torna a digitação mais lenta porque
// você precisa ficar alternando entre Shift, números e letras.
// Isso é intencional — quanto mais difícil, mais fricção.
const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%&*"

// GenerateChallenge gera uma string aleatória com o comprimento especificado.
//
// Como funciona, passo a passo:
//  1. Criamos um slice de bytes (pense nisso como uma "lista" de caracteres)
//  2. Para cada posição, usamos crypto/rand.Int() para gerar um número
//     aleatório entre 0 e o tamanho do charset
//  3. Esse número é usado como índice para pegar um caractere do charset
//  4. Repetimos isso 'length' vezes até montar a string completa
//
// Exemplo simplificado:
//
//	charset = "abc" (3 caracteres)
//	rand.Int() devolve 2 → charset[2] = 'c'
//	rand.Int() devolve 0 → charset[0] = 'a'
//	rand.Int() devolve 1 → charset[1] = 'b'
//	Resultado: "cab"
//
// No nosso caso, o charset tem 70 caracteres e o length padrão é 300,
// então o resultado é algo como: "aB3!kZ$m9Q..." (300 caracteres aleatórios).
func GenerateChallenge(length int) (string, error) {
	// big.NewInt converte o tamanho do charset para o tipo que crypto/rand espera.
	// crypto/rand trabalha com números grandes (big.Int) porque foi feito para
	// criptografia, onde os números podem ter centenas de dígitos.
	charsetSize := big.NewInt(int64(len(charset)))

	// Criamos o slice que vai guardar cada caractere gerado.
	// Em Go, um slice é como um array dinâmico — uma lista de tamanho fixo aqui.
	result := make([]byte, length)

	for i := 0; i < length; i++ {
		// rand.Int(rand.Reader, max) gera um número aleatório entre 0 e max-1.
		// rand.Reader é a fonte de aleatoriedade do sistema operacional.
		// Se algo der errado (ex: /dev/urandom não está disponível), retorna erro.
		index, err := rand.Int(rand.Reader, charsetSize)
		if err != nil {
			// Em português: "falha ao gerar número aleatório"
			// Isso praticamente nunca acontece, mas é bom tratar o erro.
			return "", fmt.Errorf("falha ao gerar caractere aleatório na posição %d: %w", i, err)
		}

		// index.Int64() converte o big.Int para um int64 normal,
		// que usamos como índice para pegar o caractere do charset.
		result[i] = charset[index.Int64()]
	}

	// Convertemos o slice de bytes para string e retornamos.
	return string(result), nil
}

// RunChallenge executa o desafio completo de digitação.
//
// Fluxo:
//  1. Gera a string aleatória com GenerateChallenge
//  2. Mostra a string formatada para o usuário (quebrada em linhas de 70 caracteres)
//  3. Lê o que o usuário digitou via stdin (teclado)
//  4. Compara caractere por caractere
//  5. Se bateu: ótimo, desbloqueio autorizado
//  6. Se não bateu: mostra exatamente onde errou para ajudar na próxima tentativa
//
// Retorna true se o desafio foi completado com sucesso, false caso contrário.
func RunChallenge(length int) (bool, error) {
	// Passo 1: Gerar o desafio
	challenge, err := GenerateChallenge(length)
	if err != nil {
		return false, fmt.Errorf("falha ao gerar desafio: %w", err)
	}

	// Passo 2: Mostrar o desafio formatado para o usuário.
	// Quebramos em linhas de 70 caracteres para facilitar a leitura.
	// Uma linha com 300 caracteres seria impossível de acompanhar.
	fmt.Println()
	fmt.Println("=== DESAFIO DE DESBLOQUEIO ===")
	fmt.Println()
	fmt.Printf("Para desbloquear, digite EXATAMENTE o texto abaixo (%d caracteres):\n", length)
	fmt.Println()
	fmt.Println(formatChallenge(challenge, 70))
	fmt.Println()

	// Passo 3: Ler a entrada do usuário.
	// Usamos bufio.Scanner porque ele lê uma linha inteira de uma vez.
	// O Scanner padrão do Go (fmt.Scan) para no primeiro espaço, o que
	// não serve para nós — nossa string pode ter qualquer caractere.
	fmt.Print("Digite o texto acima: ")
	scanner := bufio.NewScanner(os.Stdin)

	// Por padrão, o Scanner tem um limite de ~64KB por linha.
	// Para desafios muito grandes, precisamos aumentar esse buffer.
	// 1MB é mais que suficiente para qualquer desafio razoável.
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// Scan() lê uma linha do stdin. Retorna false se houver erro ou EOF.
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, fmt.Errorf("erro ao ler entrada do usuário: %w", err)
		}
		// Se Scan() retorna false sem erro, significa EOF (o usuário
		// fechou o terminal ou redirecionou /dev/null, por exemplo).
		return false, fmt.Errorf("entrada vazia — nenhum texto foi digitado")
	}

	// scanner.Text() retorna a linha lida, já sem o '\n' final.
	// Não precisamos fazer trim manual — o Scanner já cuida disso.
	input := scanner.Text()

	// Passo 4: Comparar a entrada com o desafio.
	// Primeiro fazemos uma comparação rápida (strings iguais?).
	if input == challenge {
		// Passo 5: Sucesso!
		fmt.Println()
		fmt.Println("Desbloqueio autorizado! Desafio concluído com sucesso.")
		return true, nil
	}

	// Passo 6: Falha — vamos encontrar onde está o primeiro erro
	// para ajudar o usuário na próxima tentativa.
	fmt.Println()
	fmt.Println("Texto incorreto! Desbloqueio negado.")

	// Encontramos a primeira posição onde os textos diferem.
	// Percorremos caractere por caractere até achar a diferença.
	minLen := len(input)
	if len(challenge) < minLen {
		minLen = len(challenge)
	}

	for i := 0; i < minLen; i++ {
		if input[i] != challenge[i] {
			// Mostramos a posição (começando em 1, não em 0, porque
			// para um usuário comum "posição 0" não faz sentido).
			fmt.Printf("Erro na posição %d: esperado '%c', digitado '%c'\n", i+1, challenge[i], input[i])
			return false, nil
		}
	}

	// Se chegamos aqui, um texto é prefixo do outro (um é mais curto).
	// Isso significa que o usuário digitou caracteres a mais ou a menos.
	if len(input) < len(challenge) {
		fmt.Printf("Texto muito curto! Você digitou %d caracteres, mas são necessários %d.\n", len(input), len(challenge))
	} else {
		fmt.Printf("Texto muito longo! Você digitou %d caracteres, mas são necessários %d.\n", len(input), len(challenge))
	}

	return false, nil
}

// formatChallenge quebra o texto do desafio em linhas de largura fixa.
//
// Sem isso, uma string de 300 caracteres apareceria assim no terminal:
//
//	aB3!kZ$m9Q...(...300 caracteres em uma linha só, saindo da tela)
//
// Com formatação (lineWidth=70), fica assim:
//
//	aB3!kZ$m9QxY7pL#nW2vR8dF0jH5... (70 caracteres)
//	tK4gM1sA6bC3eI9oU0wX7yZ2qJ5r... (70 caracteres)
//	fD8hN4lP0mV6kB1aS3cG9iO5uW7x... (70 caracteres)
//	eR2tY4jL6nH8pF0qZ (restante)
//
// Isso facilita muito a leitura e permite que o usuário acompanhe
// com o dedo na tela onde está enquanto digita.
func formatChallenge(text string, lineWidth int) string {
	// Se o texto é menor que a largura da linha, não precisa quebrar.
	if len(text) <= lineWidth {
		return text
	}

	// strings.Builder é a forma eficiente de concatenar strings em Go.
	// Cada vez que você faz s = s + "algo", Go cria uma string nova na memória.
	// O Builder evita isso acumulando tudo em um buffer interno.
	var builder strings.Builder

	for i := 0; i < len(text); i += lineWidth {
		// Calculamos o fim do trecho atual.
		// Se estivermos no último trecho, o fim é o tamanho total do texto.
		end := i + lineWidth
		if end > len(text) {
			end = len(text)
		}

		// Escrevemos o trecho atual.
		builder.WriteString(text[i:end])

		// Adicionamos uma quebra de linha, exceto depois do último trecho.
		if end < len(text) {
			builder.WriteByte('\n')
		}
	}

	return builder.String()
}
