import json
import os
import subprocess
import sys
import urllib.request

# Configura saida padrao do console para UTF-8 sem falhas de encoding no Windows
if sys.stdout.encoding != "utf-8":
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
CONFIG_FILENAME = "release_ai_config.json"
CONFIG_EXAMPLE_FILENAME = "release_ai_config.example.json"


def load_config():
    """Carrega as configuracoes a partir do arquivo JSON ou variaveis de ambiente."""
    # Valores padrao
    config = {
        "api_url": "http://127.0.0.1:8045/v1/chat/completions",
        "api_key": "",
        "model_name": "gemini-2.5-flash",
        "temperature": 0.3,
        "timeout_seconds": 30,
        "commit_limit": 30,
        "cleanup_keep_releases": 3,
    }

    # Procura config.json no diretorio do script ou no diretorio atual de trabalho
    config_paths = [
        os.path.join(SCRIPT_DIR, CONFIG_FILENAME),
        os.path.join(os.getcwd(), CONFIG_FILENAME),
    ]

    config_found = False
    for path in config_paths:
        if os.path.exists(path):
            try:
                with open(path, "r", encoding="utf-8") as f:
                    data = json.load(f)
                    config.update(data)
                    config_found = True
                    break
            except Exception as e:
                print(f"[Aviso] Erro ao ler '{path}': {e}")

    # Permite sobrescrever via variaveis de ambiente
    if os.getenv("AI_API_URL"):
        config["api_url"] = os.getenv("AI_API_URL")
    if os.getenv("AI_API_KEY"):
        config["api_key"] = os.getenv("AI_API_KEY")
    if os.getenv("AI_MODEL_NAME"):
        config["model_name"] = os.getenv("AI_MODEL_NAME")

    if not config_found and not config.get("api_key"):
        print(f"[Info] Arquivo '{CONFIG_FILENAME}' nao encontrado. Copie '{CONFIG_EXAMPLE_FILENAME}' para customizar.")

    return config


def get_git_commits(limit=30):
    try:
        cmd = [
            "git",
            "log",
            f"-{limit}",
            "--pretty=format:* %s (%ad)",
            "--date=short",
        ]
        result = subprocess.run(
            cmd, capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR
        )
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout.strip()
    except Exception as e:
        print(f"[Aviso] Nao foi possivel obter commits do git: {e}")
    return ""


def get_build_number():
    try:
        build_path = os.path.join(SCRIPT_DIR, "build.json")
        if os.path.exists(build_path):
            with open(build_path, "r", encoding="utf-8") as f:
                data = json.load(f)
                return str(data.get("build", "dev"))
    except Exception:
        pass
    return "dev"


def generate_notes_with_ai(build_num, commits, config):
    api_url = config.get("api_url")
    api_key = config.get("api_key", "").strip()
    model_name = config.get("model_name", "gemini-2.5-flash")
    temperature = config.get("temperature", 0.3)
    timeout = config.get("timeout_seconds", 30)

    # Se nao houver chave configurada ou estiver com texto de exemplo, nao faz a requisicao
    if not api_key or api_key in ("SEU_API_KEY_AQUI", "YOUR_API_KEY_HERE"):
        raise ValueError("Chave de API nao configurada no release_ai_config.json")

    prompt = f"""Você é um assistente de engenharia de software criando Release Notes (Notas de Lançamento) para o aplicativo BuscaTextual (um buscador de arquivos e conteúdos de alta performance em Go para Windows).

Abaixo está o histórico dos últimos commits do projeto:
{commits}

Tarefa:
Gere uma descrição resumida, profissional e organizada em Markdown para o lançamento do **Build {build_num}**.
- Destaque as principais melhorias, novos recursos e correções de bugs.
- Agrupe em tópicos objetivos (ex: Novidades e Recursos, Otimizações de Performance, Correções).
- Seja direto e amigável para o usuário final. Não mencione hashes de commit.
- Responda apenas com o conteúdo em Markdown (sem blocos ```markdown envolvendo todo o texto)."""

    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {api_key}",
    }

    payload = {
        "model": model_name,
        "messages": [
            {
                "role": "system",
                "content": (
                    "Você é um gerador técnico de release notes objetivo e"
                    " preciso."
                ),
            },
            {"role": "user", "content": prompt},
        ],
        "temperature": temperature,
    }

    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(api_url, data=data, headers=headers)

    with urllib.request.urlopen(req, timeout=timeout) as resp:
        res = json.loads(resp.read().decode("utf-8"))
        content = res["choices"][0]["message"]["content"].strip()
        if content.startswith("```markdown"):
            content = content[len("```markdown") :].strip()
        elif content.startswith("```"):
            content = content[3:].strip()
        if content.endswith("```"):
            content = content[:-3].strip()
        return content


def cleanup_old_releases(keep=3):
    print(f"\nVerificando releases no GitHub para manter apenas as {keep} ultimas...")
    try:
        cmd = ["gh", "release", "list", "--limit", "100", "--json", "tagName"]
        res = subprocess.run(cmd, capture_output=True, text=True, encoding="utf-8", cwd=SCRIPT_DIR)
        if res.returncode == 0 and res.stdout.strip():
            releases = json.loads(res.stdout)
            if len(releases) > keep:
                to_delete = releases[keep:]
                for rel in to_delete:
                    tag = rel.get("tagName")
                    if tag:
                        print(f"Removendo release antiga: {tag}...")
                        subprocess.run(["gh", "release", "delete", tag, "--yes", "--cleanup-tag"], capture_output=True, cwd=SCRIPT_DIR)
                print(f"[OK] Limpeza concluida! Mantidas as {keep} releases mais recentes.")
            else:
                print(f"[OK] Total de releases ({len(releases)}) ja esta dentro do limite (<= {keep}).")
    except Exception as e:
        print(f"[Aviso] Falha na limpeza de releases antigas: {e}")


def main():
    config = load_config()
    cleanup_keep = config.get("cleanup_keep_releases", 3)
    commit_limit = config.get("commit_limit", 30)

    if "--cleanup-only" in sys.argv:
        cleanup_old_releases(cleanup_keep)
        return

    build_num = get_build_number()
    commits = get_git_commits(commit_limit)

    print(f"Gerando Release Notes com IA para o Build {build_num}...")

    notes = ""
    try:
        if commits:
            notes = generate_notes_with_ai(build_num, commits, config)
            print("[OK] Release Notes geradas pela IA com sucesso!")
        else:
            notes = f"Release oficial do BuscaTextual - Build {build_num}"
    except Exception as e:
        print(f"[Aviso] Falha ao conectar a IA ({e}). Usando fallback automatico.")
        if commits:
            notes = f"### BuscaTextual - Build {build_num}\n\n**Commits recentes:**\n{commits}"
        else:
            notes = f"Release oficial do BuscaTextual - Build {build_num}"

    output_file = os.path.join(SCRIPT_DIR, "release_notes.txt")
    with open(output_file, "w", encoding="utf-8") as f:
        f.write(notes)

    print(f"\n--- Previa das Release Notes (Build {build_num}) ---")
    print(notes)
    print("---------------------------------------------------\n")


if __name__ == "__main__":
    main()
