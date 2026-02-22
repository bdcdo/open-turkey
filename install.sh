#!/bin/bash
# =============================================================================
# Open Turkey - Script de Instalacao
# Bloqueador de produtividade para Linux
#
# Este script automatiza a compilacao e instalacao do Open Turkey.
# Deve ser executado como root (ou via sudo).
# =============================================================================

set -euo pipefail

# --- Verifica se o script esta sendo executado como root ---
# O Open Turkey precisa de privilegios de root para:
#   - copiar o binario para /usr/local/bin
#   - instalar o servico do systemd
#   - criar o diretorio do banco de dados
if [ "$(id -u)" -ne 0 ]; then
    echo "Erro: este script precisa ser executado como root."
    echo "Uso: sudo ./install.sh"
    exit 1
fi

# --- Navega ate o diretorio do projeto ---
# Garante que os comandos make encontrem o Makefile
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# --- Compila o binario ---
echo "==> Etapa 1/2: Compilando o Open Turkey..."
make build

# --- Instala o binario, servico e configura o systemd ---
echo "==> Etapa 2/2: Instalando o Open Turkey..."
make install

# --- Mensagem de sucesso com instrucoes de uso ---
echo ""
echo "============================================="
echo "  Open Turkey instalado com sucesso!"
echo "============================================="
echo ""
echo "Comandos uteis:"
echo "  open-turkey block create <bloco>                 - Cria um bloco"
echo "  open-turkey block add-site <bloco> <dominios...> - Adiciona sites ao bloco"
echo "  open-turkey block remove-site <bloco> <dominios...> - Remove sites do bloco"
echo "  open-turkey start <bloco>                        - Ativa um bloco"
echo "  open-turkey stop <bloco>                         - Desativa um bloco (se nao estiver travado)"
echo "  open-turkey status                               - Exibe o status dos blocos"
echo ""
echo "Gerenciamento do servico:"
echo "  systemctl status open-turkey   - Ver status do servico"
echo "  systemctl restart open-turkey  - Reiniciar o servico"
echo "  journalctl -u open-turkey -f   - Acompanhar logs em tempo real"
echo ""
