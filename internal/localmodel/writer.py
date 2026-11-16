"""On Air's writing helper: loads Qwen3 4B Instruct (2507, MLX, 4-bit) once,
through mlx-lm, and writes a little text for prompts over stdin and stdout,
one JSON object a line. On Air decides what to ask and checks what it
writes; this only writes.

It opens no network port and downloads nothing: the model comes from the
Hugging Face cache, offline, and a snapshot that holds any Python file is
refused, so no code from the model's repository runs. It ends when its
standard input does, as it does when On Air quits or dies.

First line out: {"ready": true, "load_ms": n}, or {"error": "..."} and exit.
Then for each line in, {"id": n, "system": "...", "user": "...", "most": n},
one line out: {"id": n, "text": "...", "ms": n, "prompt_tokens": n}, or
{"id": n, "error": "..."}.
"""
import json
import os
import sys
import time

MODEL = "mlx-community/Qwen3-4B-Instruct-2507-4bit"
REVISION = "50d427756c6b1b2fe0c0a10f67fbda1fc8e82c1b"
FILES = ("config.json", "model.safetensors", "tokenizer.json", "tokenizer_config.json")
MOST_PROMPT = 4096  # tokens of a prompt, at most
MOST_WRITTEN = 400  # tokens written, at most


def snapshot():
    """The model's folder in the Hugging Face cache, at the pinned revision,
    found without asking the Hub anything."""
    from huggingface_hub import constants

    path = os.path.join(constants.HF_HUB_CACHE, "models--" + MODEL.replace("/", "--"), "snapshots", REVISION)
    for name in FILES:
        if not os.path.isfile(os.path.join(path, name)):
            raise RuntimeError(f"{MODEL} at {REVISION} isn't in the Hugging Face cache: run tools/setup-models")
    return path


def main():
    # Only the protocol goes to standard output. Whatever the libraries
    # print goes to standard error.
    out = sys.stdout
    sys.stdout = sys.stderr

    def say(message):
        out.write(json.dumps(message) + "\n")
        out.flush()

    os.environ["HF_HUB_OFFLINE"] = "1"
    os.environ["TRANSFORMERS_OFFLINE"] = "1"
    os.environ["HF_HUB_DISABLE_TELEMETRY"] = "1"
    try:
        started = time.perf_counter()
        path = snapshot()
        code = [name for _, _, names in os.walk(path) for name in names if name.endswith(".py")]
        if code:
            raise RuntimeError("the model's snapshot holds code, which On Air doesn't run: " + ", ".join(sorted(code)[:5]))
        from mlx_lm import generate, load

        model, tokenizer = load(path, trust_remote_code=False)

        def write(system, user, most):
            messages = [{"role": "system", "content": system}, {"role": "user", "content": user}]
            prompt = tokenizer.apply_chat_template(messages, add_generation_prompt=True, tokenize=False)
            tokens = tokenizer.encode(prompt)
            if len(tokens) > MOST_PROMPT:
                raise ValueError(f"the prompt is {len(tokens)} tokens, more than {MOST_PROMPT}")
            # Greedy, so the same prompt writes the same text.
            return generate(model, tokenizer, tokens, max_tokens=max(1, min(int(most), MOST_WRITTEN))), len(tokens)

        # One short run first, so the first question doesn't wait for the
        # GPU's code to be prepared.
        write("Reply with one word.", "Ready?", 1)
        say({"ready": True, "load_ms": round((time.perf_counter() - started) * 1000)})
    except Exception as e:  # noqa: BLE001 - every failure is said, and ends the helper
        say({"error": f"{type(e).__name__}: {e}"[:400]})
        return 1
    for line in sys.stdin:
        request = {}
        try:
            request = json.loads(line)
            started = time.perf_counter()
            text, prompt_tokens = write(str(request["system"]), str(request["user"]), request.get("most", 200))
            say({"id": request["id"], "text": text, "ms": round((time.perf_counter() - started) * 1000), "prompt_tokens": prompt_tokens})
        except Exception as e:  # noqa: BLE001 - a prompt that fails is said, and the helper goes on
            say({"id": request.get("id") if isinstance(request, dict) else None, "error": f"{type(e).__name__}: {e}"[:400]})
    return 0


if __name__ == "__main__":
    sys.exit(main())
